package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConfigurePreservesCredentialsAndInode(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("setup intentionally requires nonroot")
	}
	root := t.TempDir()
	path := filepath.Join(root, ".env")
	original := "MQTT_PASSWORD='secret $value # unchanged'\nTESLA_TELEMETRY_DIR='custom/telemetry'\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(path)
	if err := configure(root, "receiver.tailnet.ts.net"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(path)
	if !os.SameFile(before, after) {
		t.Fatal("replaced bind-mounted .env inode")
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "MQTT_PASSWORD='secret $value # unchanged'") {
		t.Fatal("credentials changed")
	}
	if err := configure(root, "receiver.tailnet.ts.net"); err != nil {
		t.Fatal("repeat setup:", err)
	}
	backup, _ := os.ReadFile(filepath.Join(root, "custom/telemetry/env-before-setup"))
	if string(backup) != original {
		t.Fatal("first private backup changed")
	}
	link, err := os.Readlink(filepath.Join(root, "custom/telemetry/server-key.pem"))
	if err != nil || link != "certificates/current/server-key.pem" {
		t.Fatalf("key link: %s %v", link, err)
	}
	if err := os.WriteFile(path, []byte("TESLA_TELEMETRY_ENABLED=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := configure(root, "other.tailnet.ts.net"); err == nil {
		t.Fatal("changed enabled installation")
	}
}

func TestSettingsRejectAmbiguity(t *testing.T) {
	for _, input := range []string{"TESLA_TELEMETRY_DIR=a\nTESLA_TELEMETRY_DIR=b", "TESLA_TELEMETRY_DIR=${HOME}", "TESLA_TELEMETRY_DIR=a # comment"} {
		if _, err := setting(strings.Split(input, "\n"), "TESLA_TELEMETRY_DIR", "default"); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}

func TestRuntimeHelperProcess(t *testing.T) {
	if os.Getenv("TEST_COMPOSE_PROCESS") == "1" {
		args := os.Args
		for len(args) > 0 && args[0] != "--" {
			args = args[1:]
		}
		args = args[1:]
		if len(args) == 1 && args[0] == "--version" {
			fmt.Println("Docker mock")
			os.Exit(0)
		}
		root := os.Getenv("TEST_COMPOSE_ROOT")
		logFile, _ := os.OpenFile(filepath.Join(root, "calls"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		fmt.Fprintln(logFile, strings.Join(args, " "))
		logFile.Close()
		joined := strings.Join(args, " ")
		switch {
		case strings.Contains(joined, "telemetry-setup configure"):
			hostname := ""
			for _, arg := range args {
				if strings.HasPrefix(arg, "SETUP_HOSTNAME=") {
					hostname = strings.TrimPrefix(arg, "SETUP_HOSTNAME=")
				}
			}
			if err := configure(root, hostname); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		case strings.Contains(joined, "telemetry-certificates hostname"):
			fmt.Println("receiver.tailnet.ts.net")
		case strings.Contains(joined, "telemetry-setup config-exists"):
			directory, _ := telemetryDirectory(root)
			if _, err := os.Stat(filepath.Join(directory, "server-config.json")); err != nil {
				os.Exit(1)
			}
		case strings.Contains(joined, "run --rm --no-deps telemetry-prepare"):
			directory, _ := telemetryDirectory(root)
			if os.WriteFile(filepath.Join(directory, "server-config.json"), []byte("{}\n"), 0600) != nil {
				os.Exit(1)
			}
		}
		os.Exit(0)
	}
	if os.Getenv("TEST_TAILSCALE_PROCESS") == "1" {
		args := os.Args
		for len(args) > 0 && args[0] != "--" {
			args = args[1:]
		}
		args = args[2:]
		if args[0] == "status" {
			fmt.Print(`{"BackendState":"Running","Self":{"DNSName":"receiver.tailnet.ts.net."},"CertDomains":["receiver.tailnet.ts.net"]}`)
			os.Exit(0)
		}
		if os.Getenv("TEST_TAILSCALE_FAIL") == "1" {
			os.Exit(1)
		}
		for _, arg := range args[1:] {
			for prefix, name := range map[string]string{"--cert-file=": "fullchain.pem", "--key-file=": "server-key.pem"} {
				if strings.HasPrefix(arg, prefix) {
					data, err := os.ReadFile(filepath.Join(os.Getenv("TEST_CANDIDATE"), name))
					if err != nil {
						os.Exit(1)
					}
					if os.WriteFile(strings.TrimPrefix(arg, prefix), data, 0600) != nil {
						os.Exit(1)
					}
				}
			}
		}
		os.Exit(0)
	}
	if os.Getenv("TEST_RECEIVER_PROCESS") != "1" {
		return
	}
	directory := filepath.Join(os.Getenv("TEST_CERT_DIRECTORY"), "current")
	pair, err := tls.LoadX509KeyPair(filepath.Join(directory, "fullchain.pem"), filepath.Join(directory, "server-key.pem"))
	if err != nil {
		os.Exit(1)
	}
	listener, err := tls.Listen("tcp", os.Getenv("TEST_RECEIVER_ADDRESS"), &tls.Config{Certificates: []tls.Certificate{pair}, ClientAuth: tls.RequireAnyClientCert})
	if err != nil {
		os.Exit(1)
	}
	for {
		connection, err := listener.Accept()
		if err != nil {
			os.Exit(1)
		}
		connection.SetDeadline(time.Now().Add(time.Second))
		connection.Write([]byte("ok"))
		connection.Close()
	}
}

func TestSetupScriptWithMockCompose(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("setup requires nonroot")
	}
	root := filepath.Join(t.TempDir(), "project with spaces")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile("../../setup-telemetry.sh")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "setup-telemetry.sh"), script, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("MQTT_PASSWORD='keep $literal # value'\nTESLA_TELEMETRY_DIR=custom/receiver\n"), 0600); err != nil {
		t.Fatal(err)
	}
	commands := t.TempDir()
	binary, _ := os.Executable()
	engine := "#!/bin/sh\nexec \"$TEST_EXECUTABLE\" -test.run=TestRuntimeHelperProcess -- \"$@\"\n"
	if err := os.WriteFile(filepath.Join(commands, "docker"), []byte(engine), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", commands+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TEST_EXECUTABLE", binary)
	t.Setenv("TEST_COMPOSE_PROCESS", "1")
	t.Setenv("TEST_COMPOSE_ROOT", root)
	for attempt := 0; attempt < 2; attempt++ {
		output, err := exec.Command("bash", filepath.Join(root, "setup-telemetry.sh"), "setup", "--yes").CombinedOutput()
		if err != nil {
			t.Fatalf("setup: %v\n%s", err, output)
		}
	}
	calls, _ := os.ReadFile(filepath.Join(root, "calls"))
	if strings.Count(string(calls), "run --rm --no-deps telemetry-prepare") != 1 {
		t.Fatal("setup prepared config more than once")
	}
	if _, err := os.Stat(filepath.Join(root, "custom/receiver/server-config.json")); err != nil {
		t.Fatal("custom directory not prepared")
	}
	if strings.Contains(string(calls), "up -d tesla-api") || strings.Contains(string(calls), "telemetry/config") {
		t.Fatal("setup attempted Tesla activation or API restart")
	}
	data, _ := os.ReadFile(filepath.Join(root, ".env"))
	if !strings.Contains(string(data), "MQTT_PASSWORD='keep $literal # value'") {
		t.Fatal("setup changed credentials")
	}
}

func TestSetupContainer(t *testing.T) {
	image := os.Getenv("TELEMETRY_WORKER_TEST_IMAGE")
	if image == "" {
		t.Skip("set TELEMETRY_WORKER_TEST_IMAGE for isolated setup container validation")
	}
	if os.Getuid() == 0 {
		t.Skip("setup requires nonroot")
	}
	engine := "docker"
	flags := []string{"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())}
	if _, err := exec.LookPath("podman"); err == nil {
		engine = "podman"
		flags = append(flags, "--userns=keep-id")
	}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, ".env"), []byte("MQTT_PASSWORD=synthetic-only\n"), 0600); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"run", "--rm", "--network", "none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges"}, flags...)
	args = append(args, "-v", directory+":/workspace", image, "configure")
	if output, err := exec.Command(engine, args...).CombinedOutput(); err != nil {
		t.Fatalf("container setup failed: %v %s", err, output)
	}
	if _, err := os.Stat(filepath.Join(directory, "config/telemetry/tailscale/funnel.json")); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(engine, "run", "--rm", "--network", "none", "--entrypoint", "tailscale", image, "version").CombinedOutput()
	if err != nil || !strings.Contains(string(output), "1.104.1") {
		t.Fatalf("Tailscale CLI unavailable: %v %s", err, output)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	fixture := t.TempDir()
	envPath := filepath.Join(fixture, ".env")
	values := "MQTT_USERNAME=test\nMQTT_PASSWORD=test\nMYSQL_ROOT_PASSWORD=test\nMYSQL_PASSWORD=test\n"
	mode := ""
	if engine == "podman" {
		mode = "keep-id"
		values += "TELEMETRY_USERNS_MODE=keep-id\n"
	}
	if err := os.WriteFile(envPath, []byte(values), 0600); err != nil {
		t.Fatal(err)
	}
	project := fmt.Sprintf("telemetry-setup-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { exec.Command(engine, "network", "rm", project+"_tesla-network").Run() })
	args = []string{"compose", "--env-file", envPath, "-p", project, "-f", filepath.Join(root, "docker-compose.yml"), "-f", filepath.Join(root, "docker-compose.tailscale.yml"), "--profile", "telemetry", "--profile", "telemetry-setup", "run", "--rm", "--no-deps", "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "telemetry-setup", "configure"}
	command := exec.Command(engine, args...)
	command.Env = append(os.Environ(), "TELEMETRY_USERNS_MODE="+mode, "TELEMETRY_SETUP_ROOT="+fixture)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Compose setup failed: %v %s", err, output)
	}
	if _, err := os.Stat(filepath.Join(fixture, "config/telemetry/tailscale/funnel.json")); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("TELEMETRY_TEST_PREPARE") != "1" {
		return
	}
	args = append(args[:len(args)-2], "telemetry-prepare")
	command = exec.Command(engine, args...)
	command.Env = append(os.Environ(), "TELEMETRY_USERNS_MODE="+mode, "TELEMETRY_SETUP_ROOT="+fixture, "TESLA_TELEMETRY_DIR="+filepath.Join(fixture, "config/telemetry"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("isolated Compose preparation failed: %v %s", err, output)
	}
	encoded, err := os.ReadFile(filepath.Join(fixture, "config/telemetry/server-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		MQTT struct{ Username, Password string }
	}
	if err := json.Unmarshal(encoded, &config); err != nil || config.MQTT.Username != "test" || config.MQTT.Password != "test" {
		t.Fatal("preparation did not reuse supplied MQTT credentials")
	}
	if _, err := os.Stat(filepath.Join(fixture, "tesla-tokens.json")); !os.IsNotExist(err) {
		t.Fatal("preparation touched tokens")
	}
}

func TestRenewalCommandFailureKeepsActiveGeneration(t *testing.T) {
	hostname := "receiver.tailnet.ts.net"
	candidate, _, _ := testBundle(t, nil, nil, hostname)
	directory := t.TempDir()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	commands := t.TempDir()
	script := "#!/bin/sh\nexec \"$TEST_EXECUTABLE\" -test.run=TestRuntimeHelperProcess -- \"$@\"\n"
	if err := os.WriteFile(filepath.Join(commands, "tailscale"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", commands+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TEST_EXECUTABLE", binary)
	t.Setenv("TEST_TAILSCALE_PROCESS", "1")
	t.Setenv("TEST_CANDIDATE", candidate)
	t.Setenv("TELEMETRY_UID", fmt.Sprint(os.Getuid()))
	t.Setenv("TELEMETRY_GID", fmt.Sprint(os.Getgid()))
	if os.Getuid() == 0 {
		t.Skip("renewal refuses root receiver UID")
	}
	previousFetcher := rootFetcher
	defer func() { rootFetcher = previousFetcher }()
	rootFetcher = func(context.Context) ([]byte, error) { return os.ReadFile(filepath.Join(candidate, "ca.pem")) }
	if err := renewOnce(context.Background(), directory); err != nil {
		t.Fatal(err)
	}
	active, _ := os.Readlink(filepath.Join(directory, "current"))
	t.Setenv("TEST_TAILSCALE_FAIL", "1")
	rootFetcher = func(context.Context) ([]byte, error) {
		t.Fatal("unnecessary CA download for valid certificate")
		return nil, nil
	}
	if err := renewOnce(context.Background(), directory); err != nil {
		t.Fatal("requested unnecessary renewal:", err)
	}
	if current, _ := os.Readlink(filepath.Join(directory, "current")); current != active {
		t.Fatal("valid generation replaced")
	}
	short, authority, authorityKey := testBundle(t, nil, nil, hostname, 7*24*time.Hour)
	shortDirectory := t.TempDir()
	if _, err := publishBundle(shortDirectory, short, hostname, os.Getuid(), os.Getgid()); err != nil {
		t.Fatal(err)
	}
	previous, _ := os.Readlink(filepath.Join(shortDirectory, "current"))
	if err := renewOnce(context.Background(), shortDirectory); err == nil {
		t.Fatal("ignored failed Tailscale renewal")
	}
	if current, _ := os.Readlink(filepath.Join(shortDirectory, "current")); current != previous {
		t.Fatal("failed renewal changed active pointer")
	}
	renewed, _, _ := testBundle(t, authority, authorityKey, hostname)
	t.Setenv("TEST_TAILSCALE_FAIL", "")
	t.Setenv("TEST_CANDIDATE", renewed)
	rootFetcher = func(context.Context) ([]byte, error) { return os.ReadFile(filepath.Join(renewed, "ca.pem")) }
	if err := renewOnce(context.Background(), shortDirectory); err != nil {
		t.Fatal("renewal recovery:", err)
	}
	if current, _ := os.Readlink(filepath.Join(shortDirectory, "current")); current == previous {
		t.Fatal("recovered renewal not published")
	}
}

func TestReceiverContainerReload(t *testing.T) {
	image := os.Getenv("TELEMETRY_TEST_IMAGE")
	if image == "" {
		t.Skip("set TELEMETRY_TEST_IMAGE to the built supervised receiver for an isolated container smoke test")
	}
	engine := "docker"
	flags := []string{"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())}
	if _, err := exec.LookPath("podman"); err == nil {
		engine = "podman"
		flags = append(flags, "--userns=keep-id")
	}
	directory := t.TempDir()
	hostname := "receiver.tailnet.ts.net"
	first, authority, key := testBundle(t, nil, nil, hostname)
	if _, err := publishBundle(directory, first, hostname, os.Getuid(), os.Getgid()); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	config := map[string]interface{}{
		"host": "127.0.0.1", "port": port, "log_level": "info", "namespace": "test",
		"records": map[string]interface{}{"V": []string{"logger"}},
		"tls":     map[string]string{"server_cert": "/etc/fleet-telemetry/current/fullchain.pem", "server_key": "/etc/fleet-telemetry/current/server-key.pem"},
	}
	encoded, _ := json.Marshal(config)
	if err := os.WriteFile(filepath.Join(directory, "server-config.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("telemetry-runtime-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { exec.Command(engine, "rm", "-f", name).Run() })
	args := append([]string{"run", "-d", "--name", name, "--network", "host", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges"}, flags...)
	args = append(args, "-e", "CERT_DIRECTORY=/etc/fleet-telemetry", "-v", directory+":/etc/fleet-telemetry:ro", image)
	if output, err := exec.Command(engine, args...).CombinedOutput(); err != nil {
		t.Fatalf("container startup: %s %v", output, err)
	}
	check := func(candidate string) {
		t.Helper()
		pair, _ := tls.LoadX509KeyPair(filepath.Join(candidate, "fullchain.pem"), filepath.Join(candidate, "server-key.pem"))
		expected := fmt.Sprintf("%x", sha256.Sum256(pair.Certificate[0]))
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			connection, err := tls.DialWithDialer(&net.Dialer{Timeout: 300 * time.Millisecond}, "tcp", address, &tls.Config{InsecureSkipVerify: true})
			if err == nil {
				connection.SetDeadline(time.Now().Add(time.Second))
				actual := fmt.Sprintf("%x", sha256.Sum256(connection.ConnectionState().PeerCertificates[0].Raw))
				_, readErr := connection.Read(make([]byte, 1))
				connection.Close()
				if actual == expected && readErr != nil && strings.Contains(readErr.Error(), "certificate required") {
					return
				}
			}
			<-time.After(100 * time.Millisecond)
		}
		logs, _ := exec.Command(engine, "logs", name).CombinedOutput()
		t.Fatalf("expected certificate with mTLS rejection not served: %s", logs)
	}
	check(first)
	renewed, _, _ := testBundle(t, authority, key, hostname)
	if _, err := publishBundle(directory, renewed, hostname, os.Getuid(), os.Getgid()); err != nil {
		t.Fatal(err)
	}
	check(renewed)
	untrusted, _, _ := testBundle(t, nil, nil, hostname)
	if _, err := publishBundle(directory, untrusted, hostname, os.Getuid(), os.Getgid()); err == nil {
		t.Fatal("untrusted CA accepted")
	}
	check(renewed)
	if output, err := exec.Command(engine, "restart", name).CombinedOutput(); err != nil {
		t.Fatalf("restart: %s %v", output, err)
	}
	check(renewed)
}

func TestReceiverReloadsCertificateAndStops(t *testing.T) {
	hostname := "receiver.tailnet.ts.net"
	candidate, authority, authorityKey := testBundle(t, nil, nil, hostname)
	directory := t.TempDir()
	if _, err := publishBundle(directory, candidate, hostname, os.Getuid(), os.Getgid()); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	binary, _ := os.Executable()
	t.Setenv("TEST_RECEIVER_PROCESS", "1")
	t.Setenv("TEST_RECEIVER_ADDRESS", address)
	t.Setenv("TEST_CERT_DIRECTORY", directory)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- supervise(ctx, directory, 20*time.Millisecond, []string{binary, "-test.run=TestRuntimeHelperProcess"})
	}()
	client, err := tls.LoadX509KeyPair(filepath.Join(candidate, "fullchain.pem"), filepath.Join(candidate, "server-key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	waitFingerprint := func(expected string) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			connection, err := tls.DialWithDialer(&net.Dialer{Timeout: 100 * time.Millisecond}, "tcp", address, &tls.Config{InsecureSkipVerify: true, Certificates: []tls.Certificate{client}})
			if err == nil {
				connection.SetDeadline(time.Now().Add(time.Second))
				response, readErr := io.ReadAll(connection)
				fingerprint := fmt.Sprintf("%x", sha256.Sum256(connection.ConnectionState().PeerCertificates[0].Raw))
				connection.Close()
				if readErr == nil && string(response) == "ok" && fingerprint == expected {
					return
				}
			}
			select {
			case <-time.After(20 * time.Millisecond):
			case err := <-done:
				t.Fatalf("receiver stopped: %v", err)
			}
		}
		t.Fatal("receiver did not serve expected certificate")
	}
	fingerprint := func(path string) string {
		pair, _ := tls.LoadX509KeyPair(filepath.Join(path, "fullchain.pem"), filepath.Join(path, "server-key.pem"))
		return fmt.Sprintf("%x", sha256.Sum256(pair.Certificate[0]))
	}
	waitFingerprint(fingerprint(candidate))
	untrusted, err := tls.Dial("tcp", address, &tls.Config{InsecureSkipVerify: true})
	if err == nil {
		untrusted.SetDeadline(time.Now().Add(time.Second))
		_, err = io.ReadAll(untrusted)
		untrusted.Close()
	}
	if err == nil {
		t.Fatal("receiver accepted connection without client certificate")
	}
	renewed, _, _ := testBundle(t, authority, authorityKey, hostname)
	if _, err := publishBundle(directory, renewed, hostname, os.Getuid(), os.Getgid()); err != nil {
		t.Fatal(err)
	}
	waitFingerprint(fingerprint(renewed))
	cancel()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("receiver did not shut down")
	}
}
