package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func setting(lines []string, name, fallback string) (string, error) {
	result := fallback
	found := false
	for _, line := range lines {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || strings.TrimSpace(strings.TrimPrefix(key, "export ")) != name {
			continue
		}
		if found {
			return "", fmt.Errorf("duplicate %s in .env; resolve before setup", name)
		}
		found = true
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"')) {
			value = value[1 : len(value)-1]
		}
		if strings.ContainsAny(value, "$`\r\n#") {
			return "", fmt.Errorf("%s must be a literal value without expansion or inline comments", name)
		}
		if value != "" {
			result = value
		}
	}
	return result, nil
}

func updateSetting(lines []string, name, value string) []string {
	for index, line := range lines {
		key, _, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && strings.TrimSpace(strings.TrimPrefix(key, "export ")) == name {
			lines[index] = name + "=" + value
			return lines
		}
	}
	return append(lines, name+"="+value)
}

func telemetryDirectory(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, ".env"))
	if err != nil {
		return "", err
	}
	directory, err := setting(strings.Split(string(data), "\n"), "TESLA_TELEMETRY_DIR", "config/telemetry")
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(directory) || filepath.Clean(directory) == "." || strings.HasPrefix(filepath.Clean(directory), "..") || strings.ContainsAny(directory, " \t") {
		return "", fmt.Errorf("telemetry directory must be a literal relative path inside the project without whitespace")
	}
	return filepath.Join(root, directory), nil
}

func configure(root, hostname string) error {
	if os.Getuid() == 0 {
		return fmt.Errorf("run setup as the nonroot owner of the project")
	}
	path := filepath.Join(root, ".env")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("existing project .env is required: %w", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	directory, err := telemetryDirectory(root)
	if err != nil {
		return err
	}
	for _, name := range []string{"TESLA_TELEMETRY_ENABLED", "TESLA_TELEMETRY_HOSTNAME", "TESLA_TELEMETRY_PORT", "TELEMETRY_UID", "TELEMETRY_GID", "TELEMETRY_USERNS_MODE"} {
		if _, err := setting(lines, name, ""); err != nil {
			return err
		}
	}
	enabled, _ := setting(lines, "TESLA_TELEMETRY_ENABLED", "false")
	if enabled != "false" {
		return fmt.Errorf("set TESLA_TELEMETRY_ENABLED=false before changing the telemetry deployment; the vehicle is not modified by setup")
	}
	if hostname != "" && (!strings.HasSuffix(hostname, ".ts.net") || strings.ContainsAny(hostname, " /:\r\n\t")) {
		return fmt.Errorf("invalid Tailscale hostname")
	}
	for parent := directory; parent != root; parent = filepath.Dir(parent) {
		if info, err := os.Lstat(parent); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("telemetry directory must not traverse symlinks")
		}
	}
	for _, name := range []string{"fullchain.pem", "server-key.pem", "ca.pem"} {
		path := filepath.Join(directory, name)
		target := filepath.Join("certificates", "current", name)
		if existing, err := os.Readlink(path); err == nil && existing == target {
			continue
		}
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("%s already exists: back up and migrate existing certificates explicitly", name)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	for _, name := range []string{"", "certificates", "tailscale"} {
		if err := os.MkdirAll(filepath.Join(directory, name), 0700); err != nil {
			return err
		}
		if err := os.Chmod(filepath.Join(directory, name), 0700); err != nil {
			return err
		}
	}
	for _, name := range []string{"fullchain.pem", "server-key.pem", "ca.pem"} {
		path := filepath.Join(directory, name)
		target := filepath.Join("certificates", "current", name)
		if existing, err := os.Readlink(path); err == nil && existing == target {
			continue
		}
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("%s already exists: back up and migrate existing certificates explicitly", name)
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.Symlink(target, path); err != nil {
			return err
		}
	}
	funnel := []byte("{\n  \"TCP\": {\"443\": {\"TCPForward\": \"fleet-telemetry:8443\"}},\n  \"AllowFunnel\": {\"${TS_CERT_DOMAIN}:443\": true}\n}\n")
	if err := atomicWrite(filepath.Join(directory, "tailscale", "funnel.json"), funnel, 0600); err != nil {
		return err
	}
	if hostname != "" {
		lines = updateSetting(lines, "TESLA_TELEMETRY_HOSTNAME", hostname)
	}
	lines = updateSetting(lines, "TESLA_TELEMETRY_ENABLED", "false")
	lines = updateSetting(lines, "TESLA_TELEMETRY_PORT", "443")
	lines = updateSetting(lines, "TELEMETRY_UID", strconv.Itoa(os.Getuid()))
	lines = updateSetting(lines, "TELEMETRY_GID", strconv.Itoa(os.Getgid()))
	if os.Getenv("TELEMETRY_USERNS_MODE") == "keep-id" {
		lines = updateSetting(lines, "TELEMETRY_USERNS_MODE", "keep-id")
	}
	updated := []byte(strings.Join(lines, "\n") + "\n")
	if string(updated) != string(data) {
		backup := filepath.Join(directory, "env-before-setup")
		if _, err := os.Stat(backup); os.IsNotExist(err) {
			if err := os.WriteFile(backup, data, 0600); err != nil {
				return err
			}
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			return err
		}
		_, writeErr := file.Write(updated)
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	fmt.Println("Telemetry files prepared; existing OAuth tokens and Fleet keys unchanged.")
	return nil
}
