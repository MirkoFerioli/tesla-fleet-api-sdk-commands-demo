package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const defaultOAuthScopes = "openid offline_access vehicle_device_data vehicle_cmds vehicle_charging_cmds"

type TelemetryConfig struct {
	Enabled      bool
	Hostname     string
	Port         string
	Directory    string
	FieldsPath   string
	MQTTUsername string
	MQTTPassword string
}

func loadTelemetryConfig(values map[string]string) TelemetryConfig {
	return TelemetryConfig{
		Enabled:      envBool(values, "TESLA_TELEMETRY_ENABLED", false),
		Hostname:     envValue(values, "TESLA_TELEMETRY_HOSTNAME", ""),
		Port:         envValue(values, "TESLA_TELEMETRY_PORT", "443"),
		Directory:    envValue(values, "TESLA_TELEMETRY_DIR", "config/telemetry"),
		FieldsPath:   envValue(values, "TESLA_TELEMETRY_FIELDS", "telemetry/vehicle-fields.json"),
		MQTTUsername: envValue(values, "MQTT_USERNAME", ""),
		MQTTPassword: envValue(values, "MQTT_PASSWORD", ""),
	}
}

func runTelemetryCLI(config Config, args []string, output io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: tesla-api --prepare-telemetry | --check-telemetry")
	}
	switch args[0] {
	case "--prepare-telemetry":
		if err := prepareTelemetry(config.Telemetry); err != nil {
			return err
		}
		_, err := fmt.Fprintln(output, "Telemetry server config prepared; install certificates and run --check-telemetry before activation. No vehicle or tokens modified.")
		return err
	case "--check-telemetry":
		port, ca, err := validateTelemetryTLS(config.Telemetry)
		if err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(map[string]interface{}{
			"hostname": config.Telemetry.Hostname, "port": port, "ca": ca,
		})
	default:
		return fmt.Errorf("unknown option %q; use --prepare-telemetry or --check-telemetry", args[0])
	}
}

func prepareTelemetry(config TelemetryConfig) error {
	if config.MQTTUsername == "" || config.MQTTPassword == "" {
		return fmt.Errorf("MQTT_USERNAME and MQTT_PASSWORD are required")
	}
	if err := os.MkdirAll(config.Directory, 0700); err != nil {
		return err
	}
	serverConfig := map[string]interface{}{
		"host": "0.0.0.0", "port": 8443, "log_level": "info", "json_log_enable": true,
		"namespace": "tesla", "reliable_ack_sources": map[string]string{"V": "mqtt"},
		"records": map[string][]string{
			"V": {"mqtt"}, "alerts": {"mqtt"}, "errors": {"mqtt"}, "connectivity": {"mqtt"},
		},
		"mqtt": map[string]interface{}{
			"broker": "mosquitto:1883", "client_id": "tesla-fleet-telemetry",
			"username": config.MQTTUsername, "password": config.MQTTPassword,
			"topic_base": "tesla", "qos": 1, "retained": false,
		},
		"tls": map[string]string{
			"server_cert": "/etc/fleet-telemetry/fullchain.pem",
			"server_key":  "/etc/fleet-telemetry/server-key.pem",
		},
	}
	encoded, err := json.MarshalIndent(serverConfig, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(config.Directory, "server-config.json")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("create telemetry config (existing files are never overwritten): %w", err)
	}
	_, writeErr := file.Write(append(encoded, '\n'))
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func validateTelemetryTLS(config TelemetryConfig) (int, string, error) {
	label := regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)
	host := config.Hostname
	if len(host) > 253 || !strings.Contains(host, ".") || net.ParseIP(host) != nil {
		return 0, "", fmt.Errorf("TESLA_TELEMETRY_HOSTNAME must be a public DNS hostname, not a URL or IP")
	}
	for _, part := range strings.Split(host, ".") {
		if !label.MatchString(part) {
			return 0, "", fmt.Errorf("invalid TESLA_TELEMETRY_HOSTNAME")
		}
	}
	for _, suffix := range []string{".localhost", ".local", ".invalid", ".example", ".test"} {
		if strings.HasSuffix(strings.ToLower(host), suffix) {
			return 0, "", fmt.Errorf("telemetry hostname must be publicly reachable")
		}
	}
	port, err := strconv.Atoi(config.Port)
	if err != nil || port < 1 || port > 65535 {
		return 0, "", fmt.Errorf("TESLA_TELEMETRY_PORT must be between 1 and 65535")
	}
	pair, err := tls.LoadX509KeyPair(filepath.Join(config.Directory, "fullchain.pem"), filepath.Join(config.Directory, "server-key.pem"))
	if err != nil {
		return 0, "", fmt.Errorf("telemetry TLS key/certificate: %w", err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return 0, "", err
	}
	ca, err := os.ReadFile(filepath.Join(config.Directory, "ca.pem"))
	if err != nil {
		return 0, "", fmt.Errorf("telemetry server trust chain ca.pem: %w", err)
	}
	roots := x509.NewCertPool()
	remaining := ca
	count := 0
	for len(strings.TrimSpace(string(remaining))) > 0 {
		block, rest := pem.Decode(remaining)
		if block == nil || block.Type != "CERTIFICATE" {
			return 0, "", fmt.Errorf("ca.pem must contain only PEM certificates")
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil || !certificate.IsCA {
			return 0, "", fmt.Errorf("ca.pem contains an invalid CA certificate")
		}
		roots.AddCert(certificate)
		remaining = rest
		count++
	}
	if count == 0 {
		return 0, "", fmt.Errorf("ca.pem is empty")
	}
	intermediates := x509.NewCertPool()
	for _, der := range pair.Certificate[1:] {
		certificate, err := x509.ParseCertificate(der)
		if err != nil {
			return 0, "", err
		}
		intermediates.AddCert(certificate)
	}
	_, err = leaf.Verify(x509.VerifyOptions{
		DNSName: host, Roots: roots, Intermediates: intermediates, CurrentTime: time.Now(),
	})
	if err != nil {
		return 0, "", fmt.Errorf("telemetry certificate verification: %w", err)
	}
	return port, string(ca), nil
}

type TelemetryField struct {
	IntervalSeconds int `json:"interval_seconds"`
}

type TelemetryRequest struct {
	Fields map[string]TelemetryField `json:"fields"`
}

func decodeTelemetryRequest(reader io.Reader) (TelemetryRequest, error) {
	var request TelemetryRequest
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, fmt.Errorf("invalid telemetry JSON: %w", err)
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		return request, fmt.Errorf("expected exactly one JSON object")
	}
	if request.Fields == nil {
		return request, fmt.Errorf("fields must be an object")
	}
	return request, nil
}

func validateTelemetryFields(fields map[string]TelemetryField, scopes string) error {
	if len(fields) == 0 || len(fields) > 100 {
		return fmt.Errorf("fields must contain between 1 and 100 signals")
	}
	namePattern := regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,99}$`)
	for name, field := range fields {
		if !namePattern.MatchString(name) || field.IntervalSeconds < 1 || field.IntervalSeconds > 86400 {
			return fmt.Errorf("invalid signal %q: use a Tesla field name and interval_seconds between 1 and 86400", name)
		}
	}
	if scopes == "" {
		scopes = defaultOAuthScopes
	}
	if _, location := fields["Location"]; location && !strings.Contains(" "+strings.Join(strings.Fields(scopes), " ")+" ", " vehicle_location ") {
		return fmt.Errorf("Location requires vehicle_location in TESLA_OAUTH_SCOPES and renewed user consent")
	}
	return nil
}

func (app *App) handleTelemetryConfig(w http.ResponseWriter, r *http.Request) {
	if !app.config.Telemetry.Enabled {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "set TESLA_TELEMETRY_ENABLED=true to manage telemetry"})
		return
	}
	if !regexp.MustCompile(`^[A-HJ-NPR-Z0-9]{17}$`).MatchString(app.config.VehicleVIN) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "TESLA_VEHICLE_VIN must be a valid 17-character VIN"})
		return
	}
	endpoint := app.config.ProxyURL + "/api/1/vehicles/" + app.config.VehicleVIN + "/fleet_telemetry_config"
	var payload interface{}
	if r.Method == http.MethodPost {
		defer r.Body.Close()
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "telemetry body must not exceed 64 KiB"})
			return
		}
		if len(strings.TrimSpace(string(body))) == 0 {
			body, err = os.ReadFile(app.config.Telemetry.FieldsPath)
			if err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "cannot read telemetry fields file: " + err.Error()})
				return
			}
		}
		request, err := decodeTelemetryRequest(strings.NewReader(string(body)))
		if err == nil {
			err = validateTelemetryFields(request.Fields, app.config.OAuthScopes)
		}
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		port, ca, err := validateTelemetryTLS(app.config.Telemetry)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		payload = map[string]interface{}{
			"vins": []string{app.config.VehicleVIN},
			"config": map[string]interface{}{
				"hostname": app.config.Telemetry.Hostname, "port": port, "ca": ca, "fields": request.Fields,
			},
		}
		endpoint = app.config.ProxyURL + "/api/1/vehicles/fleet_telemetry_config"
	}
	token, err := app.tokens.AccessToken(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	status, body, err := app.tesla.doJSON(r.Context(), r.Method, endpoint, token, payload, nil)
	if err != nil {
		writeError(w, err)
		return
	}
	if len(body) == 0 {
		w.WriteHeader(status)
		return
	}
	writeJSON(w, status, jsonRawOrString(body))
}
