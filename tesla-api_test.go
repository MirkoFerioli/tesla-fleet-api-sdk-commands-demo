package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPrepareTelemetryPreservesTokensAndEscapesCredentials(t *testing.T) {
	directory := t.TempDir()
	tokenPath := filepath.Join(directory, "tokens.json")
	if err := os.WriteFile(tokenPath, []byte("keep-token"), 0600); err != nil {
		t.Fatal(err)
	}
	config := Config{
		TokenPath: tokenPath, ResetTokenOnStart: true,
		Telemetry: TelemetryConfig{
			Directory:    filepath.Join(directory, "telemetry"),
			MQTTUsername: "existing-user", MQTTPassword: "quotes\"slash\\newline\n$secret",
		},
	}
	var output bytes.Buffer
	if err := runTelemetryCLI(config, []string{"--prepare-telemetry"}, &output); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(config.Telemetry.Directory, "server-config.json")
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		MQTT struct{ Username, Password, TopicBase string }
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.MQTT.Username != config.Telemetry.MQTTUsername || decoded.MQTT.Password != config.Telemetry.MQTTPassword {
		t.Fatal("MQTT credentials were not preserved")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("unexpected config permissions: %v", err)
	}
	if err := runTelemetryCLI(config, []string{"--prepare-telemetry"}, &output); err == nil {
		t.Fatal("preparation must not overwrite existing files")
	}
	token, err := os.ReadFile(tokenPath)
	if err != nil || string(token) != "keep-token" {
		t.Fatal("preparation modified token file")
	}
}

func TestTelemetryConfigDefaultsAndOverrides(t *testing.T) {
	config := loadTelemetryConfig(nil)
	if config.Enabled || config.Port != "443" || config.Directory != "config/telemetry" {
		t.Fatalf("unexpected defaults: %+v", config)
	}
	t.Setenv("TESLA_TELEMETRY_HOSTNAME", "telemetry.myfleet.net")
	config = loadTelemetryConfig(map[string]string{"TESLA_TELEMETRY_HOSTNAME": "file.myfleet.net"})
	if config.Hostname != "telemetry.myfleet.net" {
		t.Fatal("environment must override env file")
	}
}

func TestTelemetryReadDeleteForwardWithoutCertificatesOrWake(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			calls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != method || r.URL.Path != "/api/1/vehicles/5YJ3E1EA1KF123456/fleet_telemetry_config" {
					t.Errorf("unexpected upstream request: %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("existing OAuth token not reused")
				}
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"response":{"synced":false},"error":"upstream detail"}`))
			}))
			defer upstream.Close()
			config := Config{VehicleVIN: "5YJ3E1EA1KF123456", ProxyURL: upstream.URL, Telemetry: TelemetryConfig{Enabled: true}}
			app := &App{
				config: config, tesla: &TeslaClient{http: upstream.Client()},
				tokens: &TokenManager{loaded: true, tokens: TeslaTokens{AccessToken: "test-token", ExpiresAt: time.Now().Add(time.Hour).Unix()}},
			}
			response := httptest.NewRecorder()
			app.routes().ServeHTTP(response, httptest.NewRequest(method, "/api/vehicle/telemetry/config", nil))
			if calls != 1 || response.Code != http.StatusConflict || !bytes.Contains(response.Body.Bytes(), []byte(`"synced":false`)) {
				t.Fatalf("upstream status/body lost: calls=%d response=%s", calls, response.Body.String())
			}
		})
	}
}

func TestTelemetryRejectsBeforeOAuth(t *testing.T) {
	for _, body := range []string{`{`, `null`, `{}`, `{"fields":{}}`, `{"fields":{"Location":{"interval_seconds":10}}}`, `{"fields":{"BatteryLevel":{"interval_seconds":0}}}`, `{"fields":{"BatteryLevel":{"interval_seconds":10}},"hostname":"evil.net"}`, `{"fields":{"BatteryLevel":{"interval_seconds":10}}} {}`} {
		t.Run(body, func(t *testing.T) {
			app := &App{config: Config{
				VehicleVIN: "5YJ3E1EA1KF123456", Telemetry: TelemetryConfig{Enabled: true},
			}}
			response := httptest.NewRecorder()
			app.routes().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/vehicle/telemetry/config", bytes.NewBufferString(body)))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("expected validation error, got %d: %s", response.Code, response.Body.String())
			}
		})
	}
}

func telemetryTLSFixture(t *testing.T, expires time.Time) TelemetryConfig {
	t.Helper()
	config := TelemetryConfig{Enabled: true, Hostname: "telemetry.myfleet.net", Port: "443", Directory: t.TempDir(), FieldsPath: "telemetry/vehicle-fields.json"}
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Test CA"},
		NotBefore: time.Now().Add(-24 * time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, root, root, &rootKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	server := &x509.Certificate{
		SerialNumber: big.NewInt(2), DNSNames: []string{config.Hostname},
		NotBefore: time.Now().Add(-12 * time.Hour), NotAfter: expires,
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, server, root, &serverKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(serverKey)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"ca.pem":         pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER}),
		"fullchain.pem":  pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER}),
		"server-key.pem": pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	} {
		if err := os.WriteFile(filepath.Join(config.Directory, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return config
}

func TestTelemetryCertificateValidation(t *testing.T) {
	valid := telemetryTLSFixture(t, time.Now().Add(time.Hour))
	if port, ca, err := validateTelemetryTLS(valid); err != nil || port != 443 || !strings.Contains(ca, "BEGIN CERTIFICATE") {
		t.Fatalf("valid TLS rejected: %v", err)
	}
	for _, hostname := range []string{"localhost", "127.0.0.1", "https://telemetry.myfleet.net", "foo.local", "wrong.myfleet.net"} {
		invalid := valid
		invalid.Hostname = hostname
		if _, _, err := validateTelemetryTLS(invalid); err == nil {
			t.Fatalf("accepted invalid hostname %q", hostname)
		}
	}
	invalid := valid
	invalid.Port = "65536"
	if _, _, err := validateTelemetryTLS(invalid); err == nil {
		t.Fatal("accepted invalid port")
	}
	expired := telemetryTLSFixture(t, time.Now().Add(-time.Hour))
	if _, _, err := validateTelemetryTLS(expired); err == nil {
		t.Fatal("accepted expired certificate")
	}
	other := telemetryTLSFixture(t, time.Now().Add(time.Hour))
	otherCA, err := os.ReadFile(filepath.Join(other.Directory, "ca.pem"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(valid.Directory, "ca.pem"), otherCA, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := validateTelemetryTLS(valid); err == nil {
		t.Fatal("accepted untrusted certificate")
	}
	if err := os.WriteFile(filepath.Join(valid.Directory, "ca.pem"), []byte("not PEM"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := validateTelemetryTLS(valid); err == nil {
		t.Fatal("accepted malformed CA")
	}
}

func TestTelemetryPostUsesExistingSigningProxy(t *testing.T) {
	telemetry := telemetryTLSFixture(t, time.Now().Add(time.Hour))
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/api/1/vehicles/fleet_telemetry_config" {
			t.Errorf("wrong signing endpoint: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing existing OAuth token")
		}
		var request struct {
			VINs   []string `json:"vins"`
			Config struct {
				Hostname string                    `json:"hostname"`
				Port     int                       `json:"port"`
				CA       string                    `json:"ca"`
				Fields   map[string]TelemetryField `json:"fields"`
			} `json:"config"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if len(request.VINs) != 1 || request.VINs[0] != "5YJ3E1EA1KF123456" || request.Config.Hostname != telemetry.Hostname || request.Config.Port != 443 || request.Config.Fields["Location"].IntervalSeconds != 10 || request.Config.CA == "" {
			t.Errorf("unexpected request: %+v", request)
		}
		_, _ = w.Write([]byte(`{"response":{"updated_vehicles":0,"skipped_vehicles":{"missing_key":["5YJ3E1EA1KF123456"]}}}`))
	}))
	defer upstream.Close()
	app := &App{
		config: Config{VehicleVIN: "5YJ3E1EA1KF123456", ProxyURL: upstream.URL, OAuthScopes: defaultOAuthScopes + " vehicle_location", Telemetry: telemetry},
		tesla:  &TeslaClient{http: upstream.Client()},
		tokens: &TokenManager{loaded: true, tokens: TeslaTokens{AccessToken: "test-token", ExpiresAt: time.Now().Add(time.Hour).Unix()}},
	}
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/vehicle/telemetry/config", nil))
	if calls != 1 || response.Code != 200 || !bytes.Contains(response.Body.Bytes(), []byte(`"skipped_vehicles"`)) {
		t.Fatalf("partial upstream result not preserved: %d %s", response.Code, response.Body.String())
	}
}

func TestTelemetryDisabledAndInvalidTLSNeverAuthorize(t *testing.T) {
	for _, config := range []Config{
		{},
		{Telemetry: TelemetryConfig{Enabled: true}},
		{VehicleVIN: "5YJ3E1EA1KF123456", Telemetry: TelemetryConfig{Enabled: true}},
	} {
		app := &App{config: config}
		response := httptest.NewRecorder()
		app.routes().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/vehicle/telemetry/config", strings.NewReader(`{"fields":{"BatteryLevel":{"interval_seconds":60}}}`)))
		if response.Code != 400 && response.Code != 503 {
			t.Fatalf("unexpected status %d", response.Code)
		}
	}
}

func TestTelemetryMQTTPayloads(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js is required to exercise the Node-RED function")
	}
	script := `
const assert = require('node:assert/strict');
const fs = require('node:fs');
const flow = JSON.parse(fs.readFileSync('node-red/flows/tesla-mqtt-to-mysql.json', 'utf8'));
const transform = new Function('msg', flow.find(node => node.id === 'tesla-mqtt-prepare').func);
const input = flow.find(node => node.type === 'mqtt in');
assert.equal(input.qos, '1');
assert.equal(input.datatype, 'buffer');
for (const value of [12.3, '12.3', 'ShiftStateD', false, null, { latitude: 45.1, longitude: 9.2 }, { Status: 'CONNECTED' }]) {
  const result = transform({ topic: 'tesla/TEST/v/Signal', payload: Buffer.from(JSON.stringify(value)) });
  assert.equal(result.topic, 'INSERT INTO mqtt_messages (topic, payload) VALUES (?, ?)');
  assert.equal(result.payload[0], 'tesla/TEST/v/Signal');
  assert.deepEqual(JSON.parse(result.payload[1]), value);
}
assert.equal(JSON.parse(transform({ topic: 'tesla/legacy', payload: Buffer.from('plain text') }).payload[1]), 'plain text');
`
	if output, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("Node-RED payload test failed: %v\n%s", err, output)
	}
}

func TestTelemetryReceiverContainerSmoke(t *testing.T) {
	if os.Getenv("TESLA_TEST_CONTAINERS") != "1" {
		t.Skip("set TESLA_TEST_CONTAINERS=1 to run isolated official receiver/MQTT smoke test")
	}
	engine := "docker"
	userFlags := []string{"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())}
	if _, err := exec.LookPath("podman"); err == nil {
		engine = "podman"
		userFlags = append(userFlags, "--userns=keep-id")
	}
	run := func(args ...string) string {
		t.Helper()
		output, err := exec.Command(engine, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("container command %v failed: %v\n%s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	if image := os.Getenv("TESLA_TEST_API_IMAGE"); image != "" {
		directory := t.TempDir()
		tokenPath := filepath.Join(directory, "tokens.json")
		prepared := filepath.Join(directory, "prepared")
		if err := os.Mkdir(prepared, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(tokenPath, []byte("keep-token"), 0600); err != nil {
			t.Fatal(err)
		}
		arguments := append([]string{"run", "--rm", "--network", "none"}, userFlags...)
		arguments = append(arguments, "-v", prepared+":/prepared:rw", "-v", tokenPath+":/app/tesla-tokens.json:rw", "-e", "MQTT_USERNAME=test", "-e", "MQTT_PASSWORD=test", "-e", "TESLA_RESET_TOKEN_ON_START=true", "-e", "TESLA_TELEMETRY_DIR=/prepared", image, "./tesla-api", "--prepare-telemetry")
		run(arguments...)
		token, err := os.ReadFile(tokenPath)
		if err != nil || string(token) != "keep-token" {
			t.Fatal("container preparation modified token bind mount")
		}
		if _, err := os.Stat(filepath.Join(prepared, "server-config.json")); err != nil {
			t.Fatal(err)
		}
	}
	config := telemetryTLSFixture(t, time.Now().Add(time.Hour))
	config.MQTTUsername, config.MQTTPassword = "smoke-user", "synthetic-test-password"
	if err := prepareTelemetry(config); err != nil {
		t.Fatal(err)
	}
	brokerListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer brokerListener.Close()
	receiverListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer receiverListener.Close()
	brokerAddress := brokerListener.Addr().String()
	receiverAddress := receiverListener.Addr().String()
	serverPath := filepath.Join(config.Directory, "server-config.json")
	encoded, err := os.ReadFile(serverPath)
	if err != nil {
		t.Fatal(err)
	}
	var serverConfig map[string]interface{}
	if err := json.Unmarshal(encoded, &serverConfig); err != nil {
		t.Fatal(err)
	}
	serverConfig["mqtt"].(map[string]interface{})["broker"] = brokerAddress
	serverConfig["host"] = "127.0.0.1"
	serverConfig["port"] = receiverListener.Addr().(*net.TCPAddr).Port
	encoded, err = json.Marshal(serverConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(serverPath, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	brokerConfig := fmt.Sprintf("listener %d 127.0.0.1\nallow_anonymous false\npassword_file /fixture/passwd\nlog_dest stdout\nlog_type all\n", brokerListener.Addr().(*net.TCPAddr).Port)
	if err := os.WriteFile(filepath.Join(config.Directory, "mosquitto.conf"), []byte(brokerConfig), 0600); err != nil {
		t.Fatal(err)
	}
	prefix := fmt.Sprintf("tesla-telemetry-test-%d", time.Now().UnixNano())
	broker := prefix + "-mqtt"
	receiver := prefix + "-receiver"
	for _, name := range []string{broker, receiver} {
		t.Cleanup(func() { _ = exec.Command(engine, "rm", "-f", name).Run() })
	}
	arguments := append([]string{"run", "--rm"}, userFlags...)
	arguments = append(arguments, "-v", config.Directory+":/fixture:rw", "docker.io/eclipse-mosquitto:2", "mosquitto_passwd", "-b", "-c", "/fixture/passwd", config.MQTTUsername, config.MQTTPassword)
	run(arguments...)
	_ = brokerListener.Close()
	arguments = append([]string{"run", "-d", "--name", broker, "--network", "host"}, userFlags...)
	arguments = append(arguments, "-v", config.Directory+":/fixture:ro", "docker.io/eclipse-mosquitto:2", "mosquitto", "-c", "/fixture/mosquitto.conf")
	run(arguments...)
	_ = receiverListener.Close()
	arguments = append([]string{"run", "-d", "--name", receiver, "--network", "host", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges"}, userFlags...)
	arguments = append(arguments, "-v", config.Directory+":/etc/fleet-telemetry:ro", "docker.io/tesla/fleet-telemetry:v0.9.5", "/fleet-telemetry", "-config", "/etc/fleet-telemetry/server-config.json")
	run(arguments...)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(20 * time.Second)
	for {
		logs := run("logs", broker)
		if strings.Contains(logs, "tesla-fleet-telemetry") && strings.Contains(logs, "smoke-user") {
			break
		}
		select {
		case <-ticker.C:
		case <-deadline:
			t.Fatalf("receiver did not authenticate with MQTT: %s\n%s", logs, run("logs", receiver))
		}
	}
	ca, err := os.ReadFile(filepath.Join(config.Directory, "ca.pem"))
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(ca)
	connection, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", receiverAddress, &tls.Config{RootCAs: roots, ServerName: config.Hostname, MinVersion: tls.VersionTLS12})
	if err != nil {
		if strings.Contains(err.Error(), "certificate required") {
			return
		}
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = connection.Write([]byte("GET / HTTP/1.1\r\nHost: telemetry.myfleet.net\r\n\r\n"))
	_, err = connection.Read(make([]byte, 1))
	if err == nil || !strings.Contains(err.Error(), "certificate required") {
		t.Fatalf("receiver must reject clients without Tesla certificate, got %v", err)
	}
}

func TestResetTokenFilePreservesFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "tesla-tokens.json")
	if err := os.WriteFile(path, []byte(`{"access_token":"old"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, filepath.Join(directory, "token-link")); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := resetTokenFile(path); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("reset replaced the token file; Docker bind mounts require truncation in place")
	}
	if after.Size() != 0 {
		t.Fatal("token file was not emptied")
	}
}

func TestResetTokenFileCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tesla-tokens.json")
	if err := resetTokenFile(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 || info.Mode().Perm() != 0600 {
		t.Fatalf("unexpected token file: size=%d mode=%v", info.Size(), info.Mode().Perm())
	}
}

func TestResetTokenFileRejectsDirectory(t *testing.T) {
	directory := t.TempDir()
	marker := filepath.Join(directory, "keep")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := resetTokenFile(directory); err == nil {
		t.Fatal("expected an error for a directory token mount")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("reset removed directory contents: %v", err)
	}
}
