package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type certificateStatus struct {
	LastSuccess time.Time `json:"last_success"`
	Expires     time.Time `json:"expires"`
	Error       string    `json:"error,omitempty"`
}

var rootFetcher = fetchRoots

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	if len(os.Args) != 2 {
		log.Fatal("usage: telemetry-runtime renew|receive|health|wait|hostname|configure|status")
	}
	directory := env("CERT_DIRECTORY", "/certificates")
	var err error
	switch os.Args[1] {
	case "renew":
		err = renewLoop(ctx, directory)
	case "receive":
		err = supervise(ctx, directory, 5*time.Second, []string{"/fleet-telemetry", "-config", "/etc/fleet-telemetry/server-config.json"})
	case "hostname":
		var hostname string
		hostname, err = nodeHostname(ctx)
		if err == nil {
			fmt.Println(hostname)
		}
	case "health":
		err = checkHealth(directory)
	case "wait":
		waitCtx, stop := context.WithTimeout(ctx, 3*time.Minute)
		defer stop()
		for {
			if err = checkHealth(directory); err == nil {
				break
			}
			select {
			case <-waitCtx.Done():
				err = fmt.Errorf("certificates not ready: inspect telemetry-certificates logs")
			case <-time.After(time.Second):
				continue
			}
			break
		}
	case "configure":
		err = configure(env("SETUP_ROOT", "/workspace"), os.Getenv("SETUP_HOSTNAME"))
	case "config-exists":
		var path string
		path, err = telemetryDirectory(env("SETUP_ROOT", "/workspace"))
		if err == nil {
			_, err = os.Stat(filepath.Join(path, "server-config.json"))
		}
	case "status":
		var status []byte
		status, err = os.ReadFile(filepath.Join(directory, "status.json"))
		if err == nil {
			fmt.Print(string(status))
		}
	default:
		err = fmt.Errorf("unknown command")
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func tailscale(ctx context.Context, args ...string) ([]byte, error) {
	commandCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	args = append([]string{"--socket=" + env("TS_SOCKET", "/var/run/tailscale/tailscaled.sock")}, args...)
	output, err := exec.CommandContext(commandCtx, "tailscale", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("Tailscale command failed; check node login, HTTPS certificates and Funnel policy")
	}
	return output, nil
}

func nodeHostname(ctx context.Context) (string, error) {
	output, err := tailscale(ctx, "status", "--json")
	if err != nil {
		return "", err
	}
	var status struct {
		BackendState string
		CertDomains  []string
		Self         *struct{ DNSName string }
	}
	if err := json.Unmarshal(output, &status); err != nil {
		return "", err
	}
	if status.BackendState != "Running" || status.Self == nil {
		return "", fmt.Errorf("Tailscale login is required")
	}
	hostname := strings.TrimSuffix(status.Self.DNSName, ".")
	if !strings.HasSuffix(hostname, ".ts.net") {
		return "", fmt.Errorf("expected a Tailscale ts.net hostname")
	}
	for _, domain := range status.CertDomains {
		if hostname == domain {
			return hostname, nil
		}
	}
	return "", fmt.Errorf("enable HTTPS certificates for this Tailscale node")
}

func fetchRoots(ctx context.Context) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	var roots []byte
	for _, address := range []string{"https://letsencrypt.org/certs/isrgrootx1.pem", "https://letsencrypt.org/certs/isrg-root-x2.pem"} {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			return nil, err
		}
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, 1024*1024))
		response.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("CA download returned HTTP %d", response.StatusCode)
		}
		roots = append(roots, data...)
		roots = append(roots, '\n')
	}
	return roots, nil
}

func renewOnce(ctx context.Context, directory string) error {
	hostname, err := nodeHostname(ctx)
	if err != nil {
		return err
	}
	active := filepath.Join(directory, "current")
	previousHostname, readErr := os.ReadFile(filepath.Join(active, "hostname"))
	if readErr == nil && string(previousHostname) != hostname {
		return fmt.Errorf("Tailscale hostname changed: migrate the vehicle endpoint explicitly before replacing certificates")
	}
	trust, err := os.ReadFile(filepath.Join(active, "ca.pem"))
	if err == nil {
		certificate, validationErr := validateBundle(active, hostname, trust)
		if validationErr == nil && validFor(certificate, 14*24*time.Hour) {
			return nil
		}
	}
	candidate, err := os.MkdirTemp(directory, ".candidate-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(candidate)
	if _, err := tailscale(ctx, "cert", "--min-validity=336h", "--cert-file="+filepath.Join(candidate, "fullchain.pem"), "--key-file="+filepath.Join(candidate, "server-key.pem"), hostname); err != nil {
		return err
	}
	roots, err := rootFetcher(ctx)
	if err != nil {
		return err
	}
	chain, err := os.ReadFile(filepath.Join(candidate, "fullchain.pem"))
	if err != nil {
		return err
	}
	authorities, err := certificateAuthorities(chain, roots)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(candidate, "ca.pem"), authorities, 0600); err != nil {
		return err
	}
	uid, err := strconv.Atoi(env("TELEMETRY_UID", "1000"))
	if err != nil || uid <= 0 {
		return fmt.Errorf("TELEMETRY_UID must be nonroot")
	}
	gid, err := strconv.Atoi(env("TELEMETRY_GID", "1000"))
	if err != nil || gid < 0 {
		return fmt.Errorf("invalid TELEMETRY_GID")
	}
	changed, err := publishBundle(directory, candidate, hostname, uid, gid)
	if err == nil && changed {
		log.Print("validated certificate generation published; receiver will reload")
	}
	return err
}

func renewLoop(ctx context.Context, directory string) error {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(directory, ".renew.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("another renewal worker owns this directory")
	}
	var status certificateStatus
	if data, err := os.ReadFile(filepath.Join(directory, "status.json")); err == nil {
		json.Unmarshal(data, &status)
	}
	for {
		err := renewOnce(ctx, directory)
		delay := 24 * time.Hour
		status.Error = ""
		if err != nil {
			status.Error = err.Error()
			log.Printf("renewal failed; active generation retained: %s", status.Error)
			delay = 5 * time.Minute
		} else {
			status.LastSuccess = time.Now().UTC()
			trust, readErr := os.ReadFile(filepath.Join(directory, "current", "ca.pem"))
			host, _ := os.ReadFile(filepath.Join(directory, "current", "hostname"))
			if readErr == nil {
				if certificate, verifyErr := validateBundle(filepath.Join(directory, "current"), string(host), trust); verifyErr == nil {
					status.Expires = certificate.NotAfter
				}
			}
		}
		encoded, _ := json.MarshalIndent(status, "", "  ")
		if err := atomicWrite(filepath.Join(directory, "status.json"), append(encoded, '\n'), 0644); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}

func checkHealth(directory string) error {
	var status certificateStatus
	data, err := os.ReadFile(filepath.Join(directory, "status.json"))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &status); err != nil {
		return err
	}
	if status.Error != "" || time.Since(status.LastSuccess) > 48*time.Hour || time.Until(status.Expires) < 24*time.Hour {
		return fmt.Errorf("certificate maintenance needs attention")
	}
	return nil
}

func stopChild(command *exec.Cmd, exited <-chan error) {
	command.Process.Signal(syscall.SIGTERM)
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		command.Process.Kill()
		<-exited
	}
}

func supervise(ctx context.Context, directory string, interval time.Duration, args []string) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var child *exec.Cmd
	var exited chan error
	var fingerprint string
	defer func() {
		if child != nil {
			stopChild(child, exited)
		}
	}()
	for {
		current, resolveErr := filepath.EvalSymlinks(filepath.Join(directory, "current"))
		trust, readErr := os.ReadFile(filepath.Join(current, "ca.pem"))
		hostname, _ := os.ReadFile(filepath.Join(current, "hostname"))
		if resolveErr == nil && readErr == nil {
			if _, err := validateBundle(current, string(hostname), trust); err == nil {
				next, err := bundleFingerprint(current)
				if err == nil && (child == nil || next != fingerprint) {
					if child != nil {
						stopChild(child, exited)
						child = nil
					}
					child = exec.Command(args[0], args[1:]...)
					child.Stdout, child.Stderr = os.Stdout, os.Stderr
					if err := child.Start(); err != nil {
						child = nil
						return err
					}
					exited = make(chan error, 1)
					process := child
					go func() { exited <- process.Wait() }()
					fingerprint = next
					log.Print("receiver started with validated certificate generation")
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-exited:
			child = nil
			return fmt.Errorf("receiver exited: %v", err)
		case <-ticker.C:
		}
	}
}
