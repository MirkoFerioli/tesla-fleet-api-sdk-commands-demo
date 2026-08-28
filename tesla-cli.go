package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// TeslaTokens represents the OAuth token data
type TeslaTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
}

// isRunningOnMac checks if the code is running on macOS
func isRunningOnMac() bool {
	return runtime.GOOS == "darwin"
}

// loadEnvFile loads credentials from .env file
func loadEnvFile() (string, string, string, string) {
	clientID := ""
	clientSecret := ""
	redirectURI := ""
	vehicleID := ""

	file, err := os.Open(".env")
	if err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "#") || len(strings.TrimSpace(line)) == 0 {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			switch key {
			case "TESLA_CLIENT_ID":
				clientID = val
			case "TESLA_CLIENT_SECRET":
				clientSecret = val
			case "TESLA_REDIRECT_URI":
				redirectURI = val
			case "TESLA_VEHICLE_VIN":
				vehicleID = val
			}
		}
	}
	return clientID, clientSecret, redirectURI, vehicleID
}

// loadTokens loads tokens from tesla-tokens.json
func loadTokens() (TeslaTokens, error) {
	tokens := TeslaTokens{}
	file, err := os.Open("tesla-tokens.json")
	if err != nil {
		return tokens, err
	}
	defer file.Close()
	err = json.NewDecoder(file).Decode(&tokens)
	return tokens, err
}

// authenticateAndGetTokens performs OAuth authentication
func authenticateAndGetTokens() (TeslaTokens, error) {
	fmt.Println("\n🔐 OAuth Authentication")
	fmt.Println("======================\n")

	clientID, clientSecret, redirectURI, _ := loadEnvFile()

	reader := bufio.NewReader(os.Stdin)

	if clientID == "" {
		fmt.Print("Enter Tesla CLIENT_ID: ")
		clientID, _ = reader.ReadString('\n')
		clientID = strings.TrimSpace(clientID)
	}
	if clientSecret == "" {
		fmt.Print("Enter Tesla CLIENT_SECRET: ")
		clientSecret, _ = reader.ReadString('\n')
		clientSecret = strings.TrimSpace(clientSecret)
	}
	if redirectURI == "" {
		fmt.Print("Enter Tesla REDIRECT_URI (default: http://localhost:8080/callback): ")
		inputURI, _ := reader.ReadString('\n')
		inputURI = strings.TrimSpace(inputURI)
		if inputURI != "" {
			redirectURI = inputURI
		} else {
			redirectURI = "http://localhost:8080/callback"
		}
	}

	scopes := "openid offline_access vehicle_device_data vehicle_cmds vehicle_charging_cmds"

	// Start callback server to capture authorization code
	authCodeChan := make(chan string, 1)
	errChan := make(chan error, 1)
	var server *http.Server
	var wg sync.WaitGroup

	// Start local server to handle callback
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		errParam := r.URL.Query().Get("error")

		if errParam != "" {
			msg := fmt.Sprintf("Authorization failed: %s", errParam)
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<html><body><h1>Authorization Failed</h1><p>%s</p></body></html>`, msg)
			errChan <- fmt.Errorf(msg)
			return
		}

		if code == "" {
			msg := "No authorization code received"
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<html><body><h1>Authorization Failed</h1><p>%s</p></body></html>`, msg)
			errChan <- fmt.Errorf(msg)
			return
		}

		// Send success response to browser
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><body><h1>✅ Authorization Successful!</h1><p>You can close this window and return to the terminal.</p></body></html>`)

		// Send code to channel
		authCodeChan <- code

		// Shutdown server after callback
		go func() {
			time.Sleep(100 * time.Millisecond)
			server.Shutdown(context.Background())
		}()
	})

	server = &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Give server a moment to start
	time.Sleep(500 * time.Millisecond)

	// Build OAuth URL
	params := url.Values{}
	params.Add("client_id", clientID)
	params.Add("redirect_uri", redirectURI)
	params.Add("response_type", "code")
	params.Add("scope", scopes)
	params.Add("state", fmt.Sprintf("%d", time.Now().Unix()))

	authURL := "https://auth.tesla.com/oauth2/v3/authorize?" + params.Encode()

	fmt.Println("\n📱 Opening browser for Tesla login...")
	fmt.Println()
	fmt.Println("If browser doesn't open, copy and paste this URL:")
	fmt.Println(authURL)
	fmt.Println()

	// Try to open browser
	// Windows: use rundll32, macOS: use open, Linux: use xdg-open
	var cmd *exec.Cmd
	switch {
	case os.Getenv("OS") == "Windows_NT":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", authURL)
	case isRunningOnMac():
		cmd = exec.Command("open", authURL)
	default:
		cmd = exec.Command("xdg-open", authURL)
	}
	cmd.Run()

	fmt.Println("⏳ Waiting for authorization callback...")

	// Wait for authorization code or error
	var code string
	select {
	case code = <-authCodeChan:
		fmt.Println("✅ Authorization code received!")
	case err := <-errChan:
		return TeslaTokens{}, err
	case <-time.After(5 * time.Minute):
		return TeslaTokens{}, fmt.Errorf("authorization timeout - please try again")
	}

	// Wait for server to finish
	wg.Wait()

	if code == "" {
		return TeslaTokens{}, fmt.Errorf("no authorization code provided")
	}

	fmt.Println("\n📡 Exchanging code for tokens...")

	// Exchange code for tokens
	resp, err := http.PostForm("https://auth.tesla.com/oauth2/v3/token",
		url.Values{
			"grant_type":    {"authorization_code"},
			"client_id":     {clientID},
			"client_secret": {clientSecret},
			"code":          {code},
			"redirect_uri":  {redirectURI},
		})
	if err != nil {
		return TeslaTokens{}, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	var tokensResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&tokensResp); err != nil {
		return TeslaTokens{}, fmt.Errorf("failed to decode token response: %w", err)
	}

	if errMsg, ok := tokensResp["error"]; ok {
		return TeslaTokens{}, fmt.Errorf("token error: %v", errMsg)
	}

	accessToken, _ := tokensResp["access_token"].(string)
	refreshToken, _ := tokensResp["refresh_token"].(string)
	expiresIn, _ := tokensResp["expires_in"].(float64)
	expiresAt := time.Now().Unix() + int64(expiresIn)

	tokens := TeslaTokens{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt,
	}

	// Save tokens
	tokensPath := "tesla-tokens.json"
	// Remove if it exists as a directory (cleanup)
	if stat, err := os.Stat(tokensPath); err == nil && stat.IsDir() {
		os.RemoveAll(tokensPath)
	}
	file, err := os.OpenFile(tokensPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		fmt.Printf("⚠️  Warning: Failed to save tokens to file: %v\n", err)
	} else {
		defer file.Close()
		encoder := json.NewEncoder(file)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(tokens); err != nil {
			fmt.Printf("⚠️  Warning: Failed to encode tokens: %v\n", err)
		} else {
			fmt.Println("✅ Tokens saved to tesla-tokens.json!")
		}
	}

	return tokens, nil
}

// isVehicleAwake checks if the vehicle is awake
func isVehicleAwake(vin string, token string, proxyURL string) (bool, error) {
	endpoint := fmt.Sprintf("%s/api/1/vehicles/%s", proxyURL, vin)

	// Create HTTP client with insecure TLS
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
		Timeout: 10 * time.Second,
	}

	// Create request
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	// Send request
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse response to check state
	var responseData map[string]interface{}
	if err := json.Unmarshal(body, &responseData); err != nil {
		return false, fmt.Errorf("failed to parse response: %w", err)
	}

	// Check for error message indicating vehicle is offline/asleep
	if errMsg, ok := responseData["error"].(string); ok && errMsg != "" {
		return false, nil
	}

	// Check if we got valid vehicle response
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// Look for the vehicle data in the response
		if vehicleData, ok := responseData["response"].(map[string]interface{}); ok {
			// Check the state field
			if state, ok := vehicleData["state"].(string); ok {
				return state == "online", nil
			}
		}
	}

	return false, nil
}

// wakeVehicle sends a wake command to the vehicle
func wakeVehicle(vin string, token string, proxyURL string) error {
	endpoint := fmt.Sprintf("%s/api/1/vehicles/%s/wake_up", proxyURL, vin)

	// Create HTTP client with insecure TLS
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
		Timeout: 10 * time.Second,
	}

	// Create request with nil body for POST
	req, err := http.NewRequest("POST", endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	// Send request
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// Check if successful
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	} else {
		return fmt.Errorf("wake command failed with status %d: %s", resp.StatusCode, string(body))
	}
}

// ensureVehicleAwake checks if vehicle is awake, and wakes it if necessary
func ensureVehicleAwake(vin string, token string, proxyURL string) error {
	fmt.Print("🔍 Checking if vehicle is awake...")
	awake, err := isVehicleAwake(vin, token, proxyURL)
	if err != nil {
		fmt.Printf(" ⚠️\n   Error: %v\n", err)
	}

	if awake {
		fmt.Println(" ✅ Vehicle is awake")
		return nil
	}

	fmt.Println(" ⏸️  Vehicle is sleeping")
	fmt.Print("🚗 Sending wake command...")
	if err := wakeVehicle(vin, token, proxyURL); err != nil {
		fmt.Printf(" ❌\n   Error: %v\n", err)
		return err
	}

	fmt.Println(" ✅")
	fmt.Println("⏳ Waiting for vehicle to wake up...")

	// Retry up to 60 times with increasing intervals
	maxAttempts := 60
	for i := 0; i < maxAttempts; i++ {
		// Use exponential backoff: start at 1 second, increase gradually
		waitTime := time.Duration(1+i/5) * time.Second
		if waitTime > 5*time.Second {
			waitTime = 5 * time.Second
		}
		time.Sleep(waitTime)

		awake, err := isVehicleAwake(vin, token, proxyURL)
		if err == nil && awake {
			fmt.Println("✅ Vehicle is now awake!")
			return nil
		}

		// Show progress every 5 attempts
		if (i+1)%5 == 0 {
			fmt.Printf("   Still waiting... (%d seconds elapsed)\n", i+1)
		}
	}

	return fmt.Errorf("vehicle did not wake up after %d seconds", maxAttempts)
}

// getVehicleData fetches vehicle_data, optionally restricted to specific endpoints (e.g. "charge_state")
func getVehicleData(vin string, token string, proxyURL string, endpoints string) (map[string]interface{}, error) {
	endpoint := fmt.Sprintf("%s/api/1/vehicles/%s/vehicle_data", proxyURL, vin)
	if endpoints != "" {
		endpoint += "?endpoints=" + url.QueryEscape(endpoints)
	}

	// Create HTTP client with insecure TLS
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var responseData map[string]interface{}
	if err := json.Unmarshal(body, &responseData); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("vehicle_data request failed with status %d: %s", resp.StatusCode, string(body))
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

// sendCommand sends a command to the Tesla proxy
func sendCommand(vin string, token string, command string, proxyURL string, params map[string]interface{}) error {
	endpoint := fmt.Sprintf("%s/api/1/vehicles/%s/command/%s", proxyURL, vin, command)

	// Create HTTP client with insecure TLS
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	// Convert params to JSON
	var bodyData []byte
	if len(params) > 0 {
		bodyData, _ = json.Marshal(params)
	} else {
		bodyData = []byte("{}")
	}

	// Create request
	req, err := http.NewRequest("POST", endpoint, strings.NewReader(string(bodyData)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	// Send request
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// Check if successful
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	} else {
		return fmt.Errorf("command failed with status %d: %s", resp.StatusCode, string(body))
	}
}

func main() {
	fmt.Println("🚗 Tesla Vehicle Command CLI")
	fmt.Println("============================\n")

	// Load VIN from .env
	_, _, _, vehicleID := loadEnvFile()
	if vehicleID == "" {
		fmt.Println("❌ TESLA_VEHICLE_VIN not found in .env")
		fmt.Println("\nPlease add your vehicle VIN to .env:")
		fmt.Println("  TESLA_VEHICLE_VIN=your_vin_here")
		fmt.Println("\nYou can find your VIN in your Tesla vehicle:")
		fmt.Println("  Settings > Software")
		os.Exit(1)
	}

	fmt.Printf("✅ Vehicle VIN: %s\n\n", vehicleID)

	// Get tokens
	tokens, err := loadTokens()
	if err != nil || tokens.AccessToken == "" {
		fmt.Println("📂 No valid tokens found, performing authentication...")
		tokens, err = authenticateAndGetTokens()
		if err != nil {
			fmt.Printf("❌ Authentication failed: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Printf("✅ Connected to Tesla\n\n")

	// Main menu loop
	reader := bufio.NewReader(os.Stdin)
	proxyURL := "https://localhost:4443"

	// Check vehicle status on startup
	fmt.Println("🔄 Initial vehicle status check:")
	fmt.Println()
	if err := ensureVehicleAwake(vehicleID, tokens.AccessToken, proxyURL); err != nil {
		fmt.Printf("⚠️  Warning: Could not verify vehicle is awake: %v\n", err)
	}
	fmt.Println()

	for {
		fmt.Println("What would you like to do?")
		fmt.Println("1. Lock Doors")
		fmt.Println("2. Unlock Doors")
		fmt.Println("3. Sentry Mode ON")
		fmt.Println("4. Sentry Mode OFF")
		fmt.Println("5. Show Battery Level")
		fmt.Println("6. Show Full Vehicle Status")
		fmt.Println("7. Quit")
		fmt.Print("\nEnter choice [1-7]: ")

		choice, _ := reader.ReadString('\n')
		choice = strings.TrimSpace(choice)

		fmt.Println()

		switch choice {
		case "1":
			fmt.Println("🔒 Locking doors...")
			if err := ensureVehicleAwake(vehicleID, tokens.AccessToken, proxyURL); err != nil {
				fmt.Printf("❌ Error: Could not wake vehicle: %v\n", err)
			} else if err := sendCommand(vehicleID, tokens.AccessToken, "door_lock", proxyURL, map[string]interface{}{}); err != nil {
				fmt.Printf("❌ Error: %v\n", err)
			} else {
				fmt.Println("✅ Doors locked successfully!")
			}

		case "2":
			fmt.Println("🔓 Unlocking doors...")
			if err := ensureVehicleAwake(vehicleID, tokens.AccessToken, proxyURL); err != nil {
				fmt.Printf("❌ Error: Could not wake vehicle: %v\n", err)
			} else if err := sendCommand(vehicleID, tokens.AccessToken, "door_unlock", proxyURL, map[string]interface{}{}); err != nil {
				fmt.Printf("❌ Error: %v\n", err)
			} else {
				fmt.Println("✅ Doors unlocked successfully!")
			}

		case "3":
			fmt.Println("🛡️  Enabling Sentry Mode...")
			if err := ensureVehicleAwake(vehicleID, tokens.AccessToken, proxyURL); err != nil {
				fmt.Printf("❌ Error: Could not wake vehicle: %v\n", err)
			} else if err := sendCommand(vehicleID, tokens.AccessToken, "set_sentry_mode", proxyURL, map[string]interface{}{"on": true}); err != nil {
				fmt.Printf("❌ Error: %v\n", err)
			} else {
				fmt.Println("✅ Sentry Mode enabled!")
			}

		case "4":
			fmt.Println("🛡️  Disabling Sentry Mode...")
			if err := ensureVehicleAwake(vehicleID, tokens.AccessToken, proxyURL); err != nil {
				fmt.Printf("❌ Error: Could not wake vehicle: %v\n", err)
			} else if err := sendCommand(vehicleID, tokens.AccessToken, "set_sentry_mode", proxyURL, map[string]interface{}{"on": false}); err != nil {
				fmt.Printf("❌ Error: %v\n", err)
			} else {
				fmt.Println("✅ Sentry Mode disabled!")
			}

		case "5":
			fmt.Println("🔋 Fetching battery level...")
			if err := ensureVehicleAwake(vehicleID, tokens.AccessToken, proxyURL); err != nil {
				fmt.Printf("❌ Error: Could not wake vehicle: %v\n", err)
			} else if data, err := getVehicleData(vehicleID, tokens.AccessToken, proxyURL, "charge_state"); err != nil {
				fmt.Printf("❌ Error: %v\n", err)
			} else if chargeState, ok := data["charge_state"].(map[string]interface{}); ok {
				if level, ok := chargeState["battery_level"].(float64); ok {
					fmt.Printf("✅ Battery level: %.0f%%\n", level)
				} else {
					fmt.Println("❌ Error: battery_level not found in response")
				}
			} else {
				fmt.Println("❌ Error: charge_state not found in response")
			}

		case "6":
			fmt.Println("📊 Fetching full vehicle status...")
			if err := ensureVehicleAwake(vehicleID, tokens.AccessToken, proxyURL); err != nil {
				fmt.Printf("❌ Error: Could not wake vehicle: %v\n", err)
			} else if data, err := getVehicleData(vehicleID, tokens.AccessToken, proxyURL, ""); err != nil {
				fmt.Printf("❌ Error: %v\n", err)
			} else {
				pretty, _ := json.MarshalIndent(data, "", "  ")
				fmt.Println(string(pretty))
			}

		case "7":
			fmt.Println("Goodbye! 👋")
			return

		default:
			fmt.Println("❌ Invalid choice. Please enter 1-7.")
		}

		fmt.Println()
	}
}
