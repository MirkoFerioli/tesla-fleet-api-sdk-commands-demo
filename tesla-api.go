package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
)

type Config struct {
	ClientID          string
	ClientSecret      string
	RedirectURI       string
	VehicleVIN        string
	ProxyURL          string
	TokenPath         string
	ResetTokenOnStart bool
	ListenAddr        string
	InfluxURL         string
	InfluxToken       string
	InfluxOrg         string
	InfluxBucket      string
	TeslaTimeout      time.Duration
}

type TeslaTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
}

type TeslaClient struct {
	config Config
	http   *http.Client
}

type TokenManager struct {
	config Config
	mu     sync.Mutex
	tokens TeslaTokens
	loaded bool
}

type InfluxWriter struct {
	client influxdb2.Client
	config Config
}

type App struct {
	config       Config
	tesla        *TeslaClient
	tokens       *TokenManager
	influx       *InfluxWriter
	latestMu     sync.RWMutex
	latest       map[string]interface{}
	latestAt     time.Time
	commandRegex *regexp.Regexp
}

type CommandRequest struct {
	Command string                 `json:"command"`
	Params  map[string]interface{} `json:"params"`
	Wake    *bool                  `json:"wake,omitempty"`
}

type PollRequest struct {
	Endpoints string `json:"endpoints"`
}

func main() {
	config := loadConfig()

	if config.ResetTokenOnStart {
		if err := resetTokenFile(config.TokenPath); err != nil {
			log.Fatalf("failed to reset Tesla token file: %v", err)
		}
		log.Printf("Tesla token reset enabled; %s has been cleaned", config.TokenPath)
	}

	app := NewApp(config)
	defer app.Close()

	server := &http.Server{
		Addr:         config.ListenAddr,
		Handler:      app.routes(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 2 * time.Minute,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("Tesla API service listening on %s", config.ListenAddr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server failed: %v", err)
	}
}

func NewApp(config Config) *App {
	return &App{
		config:       config,
		tesla:        NewTeslaClient(config),
		tokens:       &TokenManager{config: config},
		influx:       NewInfluxWriter(config),
		commandRegex: regexp.MustCompile(`^[A-Za-z0-9_-]+$`),
	}
}

func (app *App) Close() {
	if app.influx != nil {
		app.influx.Close()
	}
}

func (app *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", app.handleHealth)
	mux.HandleFunc("GET /api/vehicle/latest", app.handleLatest)
	mux.HandleFunc("GET /api/vehicle/data", app.handleVehicleData)
	mux.HandleFunc("POST /api/vehicle/poll", app.handlePoll)
	mux.HandleFunc("POST /api/vehicle/command", app.handleCommand)
	return requestLogger(mux)
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s completed in %s", r.Method, r.URL.Path, time.Since(started))
	})
}

func loadConfig() Config {
	envFile := readEnvFile(".env")
	return Config{
		ClientID:          envValue(envFile, "TESLA_CLIENT_ID", ""),
		ClientSecret:      envValue(envFile, "TESLA_CLIENT_SECRET", ""),
		RedirectURI:       envValue(envFile, "TESLA_REDIRECT_URI", "http://localhost:8080/callback"),
		VehicleVIN:        envValue(envFile, "TESLA_VEHICLE_VIN", ""),
		ProxyURL:          strings.TrimRight(envValue(envFile, "TESLA_PROXY_URL", "https://localhost:4443"), "/"),
		TokenPath:         envValue(envFile, "TESLA_TOKEN_PATH", "tesla-tokens.json"),
		ResetTokenOnStart: envBool(envFile, "TESLA_RESET_TOKEN_ON_START", true),
		ListenAddr:        envValue(envFile, "TESLA_API_LISTEN_ADDR", ":8000"),
		InfluxURL:         envValue(envFile, "INFLUX_URL", ""),
		InfluxToken:       envValue(envFile, "INFLUX_TOKEN", ""),
		InfluxOrg:         envValue(envFile, "INFLUX_ORG", ""),
		InfluxBucket:      envValue(envFile, "INFLUX_BUCKET", ""),
		TeslaTimeout:      30 * time.Second,
	}
}

func readEnvFile(path string) map[string]string {
	values := map[string]string{}
	file, err := os.Open(path)
	if err != nil {
		return values
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		values[strings.TrimSpace(parts[0])] = strings.Trim(strings.TrimSpace(parts[1]), `"'`)
	}
	if err := scanner.Err(); err != nil {
		log.Printf("failed to read %s completely: %v", path, err)
	}
	return values
}

func envValue(fileValues map[string]string, key string, defaultValue string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	if value := strings.TrimSpace(fileValues[key]); value != "" {
		return value
	}
	return defaultValue
}

func envBool(fileValues map[string]string, key string, defaultValue bool) bool {
	value := strings.ToLower(envValue(fileValues, key, ""))
	if value == "" {
		return defaultValue
	}
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func resetTokenFile(path string) error {
	if path == "" {
		return nil
	}
	if stat, err := os.Stat(path); err == nil && stat.IsDir() {
		return fmt.Errorf("%s is a directory; create the host token file before starting Docker Compose", path)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	return file.Close()
}

func NewTeslaClient(config Config) *TeslaClient {
	return &TeslaClient{
		config: config,
		http: &http.Client{
			Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
			Timeout:   config.TeslaTimeout,
		},
	}
}

func NewInfluxWriter(config Config) *InfluxWriter {
	if config.InfluxURL == "" || config.InfluxToken == "" || config.InfluxOrg == "" || config.InfluxBucket == "" {
		log.Println("InfluxDB is not fully configured; writes will be skipped")
		return nil
	}
	return &InfluxWriter{client: influxdb2.NewClient(config.InfluxURL, config.InfluxToken), config: config}
}

func (writer *InfluxWriter) Close() {
	if writer.client != nil {
		writer.client.Close()
	}
}

func (writer *InfluxWriter) Health(ctx context.Context) error {
	if writer == nil || writer.client == nil {
		return fmt.Errorf("not configured")
	}
	_, err := writer.client.Health(ctx)
	return err
}

func (writer *InfluxWriter) WriteVehicleData(ctx context.Context, vin string, data map[string]interface{}) error {
	if writer == nil || writer.client == nil {
		return nil
	}
	writeAPI := writer.client.WriteAPIBlocking(writer.config.InfluxOrg, writer.config.InfluxBucket)
	now := time.Now()

	for section, sectionValue := range data {
		fields := map[string]interface{}{}
		collectInfluxFields(section, sectionValue, fields)
		if len(fields) == 0 {
			continue
		}
		point := influxdb2.NewPoint("tesla_vehicle", map[string]string{"vin": vin, "endpoint": section}, fields, now)
		if err := writeAPI.WritePoint(ctx, point); err != nil {
			return err
		}
	}
	return nil
}

func (writer *InfluxWriter) WriteCommand(ctx context.Context, vin string, command string, status string, duration time.Duration, httpStatus int, errText string) error {
	if writer == nil || writer.client == nil {
		return nil
	}
	fields := map[string]interface{}{"duration_ms": duration.Milliseconds(), "http_status": httpStatus}
	if errText != "" {
		fields["error"] = errText
	}
	point := influxdb2.NewPoint("tesla_command", map[string]string{"vin": vin, "command": command, "status": status}, fields, time.Now())
	return writer.client.WriteAPIBlocking(writer.config.InfluxOrg, writer.config.InfluxBucket).WritePoint(ctx, point)
}

func collectInfluxFields(prefix string, value interface{}, fields map[string]interface{}) {
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, nested := range typed {
			collectInfluxFields(prefix+"_"+key, nested, fields)
		}
	case string:
		if typed != "" {
			fields[prefix] = typed
		}
	case bool:
		fields[prefix] = typed
	case float64:
		fields[prefix] = typed
	case int:
		fields[prefix] = typed
	case int64:
		fields[prefix] = typed
	}
}

func (manager *TokenManager) AccessToken(ctx context.Context) (string, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	if !manager.loaded {
		if tokens, err := loadTokens(manager.config.TokenPath); err == nil {
			manager.tokens = tokens
		}
		manager.loaded = true
	}

	if manager.tokens.AccessToken != "" && time.Now().Unix() < manager.tokens.ExpiresAt-60 {
		return manager.tokens.AccessToken, nil
	}

	if manager.tokens.RefreshToken != "" {
		if tokens, err := refreshTokens(ctx, manager.config, manager.tokens.RefreshToken); err == nil {
			manager.tokens = tokens
			return tokens.AccessToken, saveTokens(manager.config.TokenPath, tokens)
		} else {
			log.Printf("refresh token failed, falling back to OAuth: %v", err)
		}
	}

	tokens, err := authenticateAndGetTokens(manager.config)
	if err != nil {
		return "", err
	}
	manager.tokens = tokens
	return tokens.AccessToken, saveTokens(manager.config.TokenPath, tokens)
}

func loadTokens(path string) (TeslaTokens, error) {
	tokens := TeslaTokens{}
	file, err := os.Open(path)
	if err != nil {
		return tokens, err
	}
	defer file.Close()
	if err := json.NewDecoder(file).Decode(&tokens); err != nil {
		return tokens, err
	}
	return tokens, nil
}

func saveTokens(path string, tokens TeslaTokens) error {
	if stat, err := os.Stat(path); err == nil && stat.IsDir() {
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(tokens)
}

func authenticateAndGetTokens(config Config) (TeslaTokens, error) {
	if config.ClientID == "" || config.ClientSecret == "" || config.RedirectURI == "" {
		return TeslaTokens{}, fmt.Errorf("TESLA_CLIENT_ID, TESLA_CLIENT_SECRET and TESLA_REDIRECT_URI are required")
	}

	redirectURL, err := url.Parse(config.RedirectURI)
	if err != nil {
		return TeslaTokens{}, fmt.Errorf("invalid TESLA_REDIRECT_URI: %w", err)
	}
	callbackPath := redirectURL.Path
	if callbackPath == "" {
		callbackPath = "/callback"
	}
	serverAddr := callbackListenAddr(redirectURL)

	authCodeChan := make(chan string, 1)
	errChan := make(chan error, 1)
	var server *http.Server
	var wg sync.WaitGroup

	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if errParam := r.URL.Query().Get("error"); errParam != "" {
			http.Error(w, "Authorization failed: "+errParam, http.StatusBadRequest)
			errChan <- fmt.Errorf("authorization failed: %s", errParam)
			return
		}
		if code == "" {
			http.Error(w, "No authorization code received", http.StatusBadRequest)
			errChan <- fmt.Errorf("no authorization code received")
			return
		}

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><h1>Authorization Successful</h1><p>You can close this window and return to the terminal.</p></body></html>`)
		authCodeChan <- code

		go func() {
			time.Sleep(100 * time.Millisecond)
			_ = server.Shutdown(context.Background())
		}()
	})

	server = &http.Server{Addr: serverAddr, Handler: mux}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	params := url.Values{}
	params.Add("client_id", config.ClientID)
	params.Add("redirect_uri", config.RedirectURI)
	params.Add("response_type", "code")
	params.Add("scope", "openid offline_access vehicle_device_data vehicle_cmds vehicle_charging_cmds")
	params.Add("state", fmt.Sprintf("%d", time.Now().Unix()))
	authURL := "https://auth.tesla.com/oauth2/v3/authorize?" + params.Encode()

	log.Println("Open this URL to authorize the Tesla API service:")
	log.Println(authURL)
	openBrowser(authURL)

	var code string
	select {
	case code = <-authCodeChan:
		log.Println("Tesla authorization code received")
	case err := <-errChan:
		return TeslaTokens{}, err
	case <-time.After(5 * time.Minute):
		_ = server.Shutdown(context.Background())
		return TeslaTokens{}, fmt.Errorf("authorization timeout")
	}
	wg.Wait()

	return exchangeCodeForTokens(context.Background(), config, code)
}

func callbackListenAddr(redirectURL *url.URL) string {
	host := redirectURL.Host
	if !strings.Contains(host, ":") {
		return ":80"
	}
	_, port, err := net.SplitHostPort(host)
	if err != nil || port == "" {
		return ":8080"
	}
	return ":" + port
}

func openBrowser(authURL string) {
	var cmd *exec.Cmd
	switch {
	case os.Getenv("OS") == "Windows_NT":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", authURL)
	case runtime.GOOS == "darwin":
		cmd = exec.Command("open", authURL)
	default:
		cmd = exec.Command("xdg-open", authURL)
	}
	if err := cmd.Run(); err != nil {
		log.Printf("browser auto-open skipped: %v", err)
	}
}

func exchangeCodeForTokens(ctx context.Context, config Config, code string) (TeslaTokens, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {config.ClientID},
		"client_secret": {config.ClientSecret},
		"code":          {code},
		"redirect_uri":  {config.RedirectURI},
	}
	return tokenRequest(ctx, form)
}

func refreshTokens(ctx context.Context, config Config, refreshToken string) (TeslaTokens, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {config.ClientID},
		"client_secret": {config.ClientSecret},
		"refresh_token": {refreshToken},
	}
	return tokenRequest(ctx, form)
}

func tokenRequest(ctx context.Context, form url.Values) (TeslaTokens, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://auth.tesla.com/oauth2/v3/token", strings.NewReader(form.Encode()))
	if err != nil {
		return TeslaTokens{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return TeslaTokens{}, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return TeslaTokens{}, err
	}

	var tokensResp map[string]interface{}
	if err := json.Unmarshal(body, &tokensResp); err != nil {
		return TeslaTokens{}, fmt.Errorf("failed to decode token response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return TeslaTokens{}, fmt.Errorf("token request failed with status %d: %s", resp.StatusCode, string(body))
	}
	if errMsg, ok := tokensResp["error"]; ok {
		return TeslaTokens{}, fmt.Errorf("token error: %v", errMsg)
	}

	accessToken, _ := tokensResp["access_token"].(string)
	refreshToken, _ := tokensResp["refresh_token"].(string)
	expiresIn, _ := tokensResp["expires_in"].(float64)
	if accessToken == "" {
		return TeslaTokens{}, fmt.Errorf("token response did not include access_token")
	}

	return TeslaTokens{AccessToken: accessToken, RefreshToken: refreshToken, ExpiresAt: time.Now().Unix() + int64(expiresIn)}, nil
}

func (client *TeslaClient) ProxyHealth(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, client.config.ProxyURL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := client.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("proxy health returned %d", resp.StatusCode)
	}
	return nil
}

func (client *TeslaClient) IsVehicleAwake(ctx context.Context, vin string, token string) (bool, error) {
	endpoint := fmt.Sprintf("%s/api/1/vehicles/%s", client.config.ProxyURL, url.PathEscape(vin))
	var responseData map[string]interface{}
	status, _, err := client.doJSON(ctx, http.MethodGet, endpoint, token, nil, &responseData)
	if err != nil {
		return false, err
	}
	if status < 200 || status >= 300 {
		return false, nil
	}
	if errMsg, ok := responseData["error"].(string); ok && errMsg != "" {
		return false, nil
	}
	if vehicleData, ok := responseData["response"].(map[string]interface{}); ok {
		if state, ok := vehicleData["state"].(string); ok {
			return state == "online", nil
		}
	}
	return false, nil
}

func (client *TeslaClient) WakeVehicle(ctx context.Context, vin string, token string) error {
	endpoint := fmt.Sprintf("%s/api/1/vehicles/%s/wake_up", client.config.ProxyURL, url.PathEscape(vin))
	status, body, err := client.doJSON(ctx, http.MethodPost, endpoint, token, map[string]interface{}{}, nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("wake command failed with status %d: %s", status, string(body))
	}
	return nil
}

func (client *TeslaClient) EnsureVehicleAwake(ctx context.Context, vin string, token string) error {
	awake, err := client.IsVehicleAwake(ctx, vin, token)
	if err != nil {
		log.Printf("vehicle awake check failed: %v", err)
	}
	if awake {
		return nil
	}
	if err := client.WakeVehicle(ctx, vin, token); err != nil {
		return err
	}

	for attempt := 0; attempt < 60; attempt++ {
		waitTime := time.Duration(1+attempt/5) * time.Second
		if waitTime > 5*time.Second {
			waitTime = 5 * time.Second
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
		}
		awake, err := client.IsVehicleAwake(ctx, vin, token)
		if err == nil && awake {
			return nil
		}
	}
	return fmt.Errorf("vehicle did not wake up")
}

func (client *TeslaClient) GetVehicleData(ctx context.Context, vin string, token string, endpoints string) (map[string]interface{}, error) {
	endpoint := fmt.Sprintf("%s/api/1/vehicles/%s/vehicle_data", client.config.ProxyURL, url.PathEscape(vin))
	if endpoints != "" {
		endpoint += "?endpoints=" + url.QueryEscape(endpoints)
	}

	var responseData map[string]interface{}
	status, body, err := client.doJSON(ctx, http.MethodGet, endpoint, token, nil, &responseData)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("vehicle_data request failed with status %d: %s", status, string(body))
	}
	if errMsg, ok := responseData["error"].(string); ok && errMsg != "" {
		return nil, fmt.Errorf("vehicle_data error: %s", errMsg)
	}
	data, ok := responseData["response"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected response format")
	}
	return data, nil
}

func (client *TeslaClient) SendCommand(ctx context.Context, vin string, token string, command string, params map[string]interface{}) (int, []byte, error) {
	endpoint := fmt.Sprintf("%s/api/1/vehicles/%s/command/%s", client.config.ProxyURL, url.PathEscape(vin), url.PathEscape(command))
	status, body, err := client.doJSON(ctx, http.MethodPost, endpoint, token, params, nil)
	if err != nil {
		return status, body, err
	}
	if status < 200 || status >= 300 {
		return status, body, fmt.Errorf("command failed with status %d: %s", status, string(body))
	}
	return status, body, nil
}

func (client *TeslaClient) doJSON(ctx context.Context, method string, endpoint string, token string, payload interface{}, target interface{}) (int, []byte, error) {
	var bodyReader io.Reader
	if payload != nil {
		body, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, err
		}
		bodyReader = strings.NewReader(string(body))
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader)
	if err != nil {
		return 0, nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if target != nil && len(body) > 0 {
		if err := json.Unmarshal(body, target); err != nil {
			return resp.StatusCode, body, err
		}
	}
	return resp.StatusCode, body, nil
}

func (app *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	proxyStatus := "ok"
	if err := app.tesla.ProxyHealth(ctx); err != nil {
		proxyStatus = err.Error()
	}

	influxStatus := "not_configured"
	if app.influx != nil {
		influxStatus = "ok"
		if err := app.influx.Health(ctx); err != nil {
			influxStatus = err.Error()
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":               "ok",
		"vehicle_vin_set":      app.config.VehicleVIN != "",
		"proxy":                proxyStatus,
		"influxdb":             influxStatus,
		"reset_token_on_start": app.config.ResetTokenOnStart,
	})
}

func (app *App) handleLatest(w http.ResponseWriter, r *http.Request) {
	app.latestMu.RLock()
	defer app.latestMu.RUnlock()
	if app.latest == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no vehicle data has been polled yet"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"timestamp": app.latestAt.Format(time.RFC3339), "data": app.latest})
}

func (app *App) handleVehicleData(w http.ResponseWriter, r *http.Request) {
	data, err := app.fetchVehicleData(r.Context(), r.URL.Query().Get("endpoints"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": data})
}

func (app *App) handlePoll(w http.ResponseWriter, r *http.Request) {
	endpoints := r.URL.Query().Get("endpoints")
	if r.Body != nil {
		defer r.Body.Close()
		var request PollRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil && err != io.EOF {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
		if request.Endpoints != "" {
			endpoints = request.Endpoints
		}
	}

	data, err := app.fetchVehicleData(r.Context(), endpoints)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := app.influx.WriteVehicleData(r.Context(), app.config.VehicleVIN, data); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "influxdb write failed: " + err.Error()})
		return
	}

	app.latestMu.Lock()
	app.latest = data
	app.latestAt = time.Now()
	app.latestMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "ok", "timestamp": app.latestAt.Format(time.RFC3339), "written": true})
}

func (app *App) handleCommand(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var request CommandRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	request.Command = strings.TrimSpace(request.Command)
	if request.Command == "" || !app.commandRegex.MatchString(request.Command) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "command must contain only letters, numbers, underscore or dash"})
		return
	}
	if request.Params == nil {
		request.Params = map[string]interface{}{}
	}
	wake := true
	if request.Wake != nil {
		wake = *request.Wake
	}

	started := time.Now()
	status := "ok"
	httpStatus := 0
	errText := ""

	token, err := app.tokens.AccessToken(r.Context())
	if err == nil && wake {
		err = app.tesla.EnsureVehicleAwake(r.Context(), app.config.VehicleVIN, token)
	}
	var body []byte
	if err == nil {
		httpStatus, body, err = app.tesla.SendCommand(r.Context(), app.config.VehicleVIN, token, request.Command, request.Params)
	}
	if err != nil {
		status = "error"
		errText = err.Error()
	}
	if writeErr := app.influx.WriteCommand(r.Context(), app.config.VehicleVIN, request.Command, status, time.Since(started), httpStatus, errText); writeErr != nil {
		log.Printf("failed to write command audit to InfluxDB: %v", writeErr)
	}
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "ok", "command": request.Command, "http_status": httpStatus, "response": jsonRawOrString(body)})
}

func (app *App) fetchVehicleData(ctx context.Context, endpoints string) (map[string]interface{}, error) {
	if app.config.VehicleVIN == "" {
		return nil, fmt.Errorf("TESLA_VEHICLE_VIN is required")
	}
	token, err := app.tokens.AccessToken(ctx)
	if err != nil {
		return nil, err
	}
	return app.tesla.GetVehicleData(ctx, app.config.VehicleVIN, token, endpoints)
}

func jsonRawOrString(body []byte) interface{} {
	if len(body) == 0 {
		return nil
	}
	var value interface{}
	if err := json.Unmarshal(body, &value); err == nil {
		return value
	}
	return string(body)
}

func writeError(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("failed to write response: %v", err)
	}
}
