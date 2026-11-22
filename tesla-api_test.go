package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestVehicleWakeRoutes(t *testing.T) {
	const vin = "5YJ3E1EA1KF123456"
	vehiclePath := "/api/1/vehicles/" + vin
	for _, initialState := range []string{"asleep", "online"} {
		for _, test := range []struct {
			name, method, path, body, operation string
			emptyCache                          bool
		}{
			{name: "data", method: "GET", path: "/api/vehicle/data?endpoints=charge_state", operation: "GET " + vehiclePath + "/vehicle_data"},
			{name: "poll", method: "POST", path: "/api/vehicle/poll", body: `{"endpoints":"charge_state"}`, operation: "GET " + vehiclePath + "/vehicle_data"},
			{name: "command", method: "POST", path: "/api/vehicle/command", body: `{"command":"door_lock"}`, operation: "POST " + vehiclePath + "/command/door_lock"},
			{name: "command-wake-false", method: "POST", path: "/api/vehicle/command", body: `{"command":"door_lock","wake":false}`, operation: "POST " + vehiclePath + "/command/door_lock"},
			{name: "latest", method: "GET", path: "/api/vehicle/latest"},
			{name: "latest-empty", method: "GET", path: "/api/vehicle/latest", emptyCache: true},
		} {
			t.Run(initialState+"/"+test.name, func(t *testing.T) {
				var mutex sync.Mutex
				var calls []string
				state := initialState
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mutex.Lock()
					defer mutex.Unlock()
					call := r.Method + " " + r.URL.Path
					calls = append(calls, call)
					if r.Header.Get("Authorization") != "Bearer test-token" {
						t.Error("missing existing OAuth token")
					}
					switch call {
					case "GET " + vehiclePath:
						writeJSON(w, http.StatusOK, map[string]interface{}{"response": map[string]string{"state": state}})
					case "POST " + vehiclePath + "/wake_up":
						state = "online"
						_, _ = w.Write([]byte(`{"response":{"state":"online"}}`))
					case test.operation:
						if state != "online" {
							http.Error(w, "vehicle is asleep", http.StatusServiceUnavailable)
							return
						}
						if strings.HasSuffix(r.URL.Path, "/vehicle_data") && r.URL.Query().Get("endpoints") != "charge_state" {
							t.Error("endpoints not preserved")
						}
						_, _ = w.Write([]byte(`{"response":{"charge_state":{"battery_level":80}}}`))
					default:
						t.Errorf("unexpected upstream call: %s", call)
						http.Error(w, "unexpected request", http.StatusNotFound)
					}
				}))
				defer upstream.Close()
				config := Config{VehicleVIN: vin, ProxyURL: upstream.URL, TeslaTimeout: time.Second}
				app := NewApp(config)
				defer app.Close()
				app.tokens = &TokenManager{loaded: true, tokens: TeslaTokens{AccessToken: "test-token", ExpiresAt: time.Now().Add(time.Hour).Unix()}}
				snapshotTime := time.Now().Add(-time.Hour)
				if !test.emptyCache {
					app.latest = map[string]interface{}{"cached": true}
					app.latestAt = snapshotTime
				}
				response := httptest.NewRecorder()
				app.routes().ServeHTTP(response, httptest.NewRequest(test.method, test.path, strings.NewReader(test.body)))
				wantStatus := http.StatusOK
				if test.emptyCache {
					wantStatus = http.StatusNotFound
				}
				if response.Code != wantStatus {
					t.Errorf("status=%d want=%d body=%s", response.Code, wantStatus, response.Body.String())
				}
				wantCalls := []string{"GET " + vehiclePath}
				if initialState != "online" {
					wantCalls = append(wantCalls, "POST "+vehiclePath+"/wake_up", "GET "+vehiclePath)
				}
				if test.operation != "" {
					wantCalls = append(wantCalls, test.operation)
				}
				mutex.Lock()
				defer mutex.Unlock()
				if !reflect.DeepEqual(calls, wantCalls) {
					t.Errorf("calls=%v want=%v", calls, wantCalls)
				}
				if test.name == "latest" && (!bytes.Contains(response.Body.Bytes(), []byte(`"cached":true`)) || !app.latestAt.Equal(snapshotTime)) {
					t.Error("latest must return the unchanged cached snapshot")
				}
				if test.name == "poll" && app.latest["charge_state"] == nil {
					t.Error("successful poll did not update cache")
				}
			})
		}
	}
}

func TestVehicleWakeFailures(t *testing.T) {
	for _, test := range []struct {
		name, statusBody, wakeBody string
		statusCode, wakeCode       int
		cancelAfterWake            bool
	}{
		{name: "status-http", statusCode: 401, statusBody: `{"error":"unauthorized"}`},
		{name: "status-error", statusCode: 200, statusBody: `{"error":"upstream failure"}`},
		{name: "status-missing", statusCode: 200, statusBody: `{"response":{}}`},
		{name: "status-empty", statusCode: 200, statusBody: `{"response":{"state":""}}`},
		{name: "status-malformed", statusCode: 200, statusBody: `{`},
		{name: "wake-http", statusCode: 200, statusBody: `{"response":{"state":"asleep"}}`, wakeCode: 503, wakeBody: `{"error":"wake failed"}`},
		{name: "cancelled", statusCode: 200, statusBody: `{"response":{"state":"asleep"}}`, wakeCode: 200, wakeBody: `{"response":{}}`, cancelAfterWake: true},
		{name: "deadline", statusCode: 200, statusBody: `{"response":{"state":"asleep"}}`, wakeCode: 200, wakeBody: `{"response":{}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			calls := make(chan string, 8)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls <- r.Method + " " + r.URL.Path
				if strings.HasSuffix(r.URL.Path, "/wake_up") {
					if test.cancelAfterWake {
						cancel()
					}
					writeJSON(w, test.wakeCode, jsonRawOrString([]byte(test.wakeBody)))
					return
				}
				w.WriteHeader(test.statusCode)
				_, _ = w.Write([]byte(test.statusBody))
			}))
			defer upstream.Close()
			app := NewApp(Config{VehicleVIN: "5YJ3E1EA1KF123456", ProxyURL: upstream.URL, TeslaTimeout: time.Second})
			defer app.Close()
			app.tokens = &TokenManager{loaded: true, tokens: TeslaTokens{AccessToken: "test-token", ExpiresAt: time.Now().Add(time.Hour).Unix()}}
			app.latest = map[string]interface{}{"cached": true}
			response := httptest.NewRecorder()
			app.routes().ServeHTTP(response, httptest.NewRequest("POST", "/api/vehicle/poll", nil).WithContext(ctx))
			if response.Code != http.StatusBadGateway || !bytes.Contains(response.Body.Bytes(), []byte(`"error"`)) {
				t.Errorf("expected wake error, got %d %s", response.Code, response.Body.String())
			}
			wantCalls := 1
			if test.wakeCode != 0 {
				wantCalls = 2
			}
			if len(calls) != wantCalls {
				t.Errorf("upstream calls=%d want=%d", len(calls), wantCalls)
			}
			if !reflect.DeepEqual(app.latest, map[string]interface{}{"cached": true}) {
				t.Error("failed poll modified cache")
			}
		})
	}
}

func TestVehicleWakeFailureBlocksOperations(t *testing.T) {
	for _, test := range []struct{ method, path, body string }{
		{"GET", "/api/vehicle/data", ""},
		{"GET", "/api/vehicle/latest", ""},
		{"POST", "/api/vehicle/command", `{"command":"door_lock","wake":false}`},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			calls := make(chan string, 8)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls <- r.Method + " " + r.URL.Path
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			}))
			defer upstream.Close()
			config := Config{VehicleVIN: "5YJ3E1EA1KF123456", ProxyURL: upstream.URL, TeslaTimeout: time.Second}
			app := NewApp(config)
			defer app.Close()
			app.tokens = &TokenManager{loaded: true, tokens: TeslaTokens{AccessToken: "test-token", ExpiresAt: time.Now().Add(time.Hour).Unix()}}
			response := httptest.NewRecorder()
			app.routes().ServeHTTP(response, httptest.NewRequest(test.method, test.path, strings.NewReader(test.body)))
			if response.Code != http.StatusBadGateway || len(calls) != 1 {
				t.Fatalf("expected readiness error only: status=%d calls=%d body=%s", response.Code, len(calls), response.Body.String())
			}
			if call := <-calls; call != "GET /api/1/vehicles/5YJ3E1EA1KF123456" {
				t.Errorf("operation was not blocked: %s", call)
			}
		})
	}
}

func TestVehicleWakeValidationBeforeOAuth(t *testing.T) {
	for _, test := range []struct {
		method, path, body string
		status             int
	}{
		{method: "POST", path: "/api/vehicle/poll", body: `{`, status: 400},
		{method: "POST", path: "/api/vehicle/command", body: `{`, status: 400},
		{method: "POST", path: "/api/vehicle/command", body: `{"command":"invalid/path"}`, status: 400},
		{method: "GET", path: "/api/vehicle/data", status: 502},
		{method: "GET", path: "/api/vehicle/latest", status: 502},
		{method: "POST", path: "/api/vehicle/poll", status: 502},
		{method: "POST", path: "/api/vehicle/command", body: `{"command":"door_lock"}`, status: 502},
	} {
		t.Run(test.method+test.path+test.body, func(t *testing.T) {
			app := NewApp(Config{})
			defer app.Close()
			app.tokens = nil
			app.tesla = nil
			response := httptest.NewRecorder()
			app.routes().ServeHTTP(response, httptest.NewRequest(test.method, test.path, strings.NewReader(test.body)))
			if response.Code != test.status {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.status, response.Body.String())
			}
		})
	}
	app := &App{config: Config{VehicleVIN: "5YJ3E1EA1KF123456"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := app.readyVehicleToken(ctx); err != context.Canceled {
		t.Fatalf("cancelled request should stop before OAuth: %v", err)
	}
}

func TestRemovedTelemetryEndpointsReturnNotFound(t *testing.T) {
	app := &App{}
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			response := httptest.NewRecorder()
			app.routes().ServeHTTP(response, httptest.NewRequest(method, "/api/vehicle/telemetry/config", nil))
			if response.Code != http.StatusNotFound {
				t.Fatalf("status=%d want=%d", response.Code, http.StatusNotFound)
			}
		})
	}
}

func TestRetiredOptionsDoNotResetToken(t *testing.T) {
	const helper = "TESLA_TEST_UNEXPECTED_ARGS_HELPER"
	if option := os.Getenv(helper); option != "" {
		os.Args = []string{"tesla-api", option}
		main()
		return
	}

	for _, option := range []string{"--prepare-telemetry", "--check-telemetry"} {
		t.Run(option, func(t *testing.T) {
			tokenPath := filepath.Join(t.TempDir(), "tesla-tokens.json")
			if err := os.WriteFile(tokenPath, []byte("keep-token"), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRetiredOptionsDoNotResetToken$")
			cmd.Env = append(os.Environ(), helper+"="+option, "TESLA_TOKEN_PATH="+tokenPath, "TESLA_RESET_TOKEN_ON_START=true", "TESLA_API_LISTEN_ADDR=127.0.0.1:0")
			output, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("retired option started the service: %v", ctx.Err())
			}
			if err == nil || !bytes.Contains(output, []byte("unexpected argument")) {
				t.Fatalf("retired option not rejected: err=%v output=%s", err, output)
			}
			token, err := os.ReadFile(tokenPath)
			if err != nil || string(token) != "keep-token" {
				t.Fatalf("retired option modified the token file: %v", err)
			}
		})
	}
}

func TestMQTTPayloads(t *testing.T) {
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
