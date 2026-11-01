package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

func main() {
	args := os.Args[1:]
	certificate := flagValue(args, "-cert", os.Getenv("TESLA_HTTP_PROXY_TLS_CERT"))
	key := flagValue(args, "-tls-key", os.Getenv("TESLA_HTTP_PROXY_TLS_KEY"))
	if err := ensureCertificate(certificate, key, time.Now()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binary, err := exec.LookPath("tesla-http-proxy")
	if err != nil {
		fmt.Fprintln(os.Stderr, "find tesla-http-proxy:", err)
		os.Exit(1)
	}
	if err := syscall.Exec(binary, append([]string{binary}, args...), os.Environ()); err != nil {
		fmt.Fprintln(os.Stderr, "start tesla-http-proxy:", err)
		os.Exit(1)
	}
}

func flagValue(args []string, name, fallback string) string {
	for index, arg := range args {
		if arg == name && index+1 < len(args) {
			return args[index+1]
		}
		if value, ok := strings.CutPrefix(arg, name+"="); ok {
			return value
		}
	}
	return fallback
}

func ensureCertificate(certificatePath, keyPath string, now time.Time) error {
	if certificatePath == "" || keyPath == "" {
		return fmt.Errorf("proxy certificate and key paths must be supplied by flags or environment")
	}
	_, certErr := os.Stat(certificatePath)
	_, keyErr := os.Stat(keyPath)
	certExists := certErr == nil
	keyExists := keyErr == nil
	if certErr != nil && !os.IsNotExist(certErr) {
		return fmt.Errorf("inspect proxy certificate: %w", certErr)
	}
	if keyErr != nil && !os.IsNotExist(keyErr) {
		return fmt.Errorf("inspect proxy TLS key: %w", keyErr)
	}
	if certExists && keyExists {
		return nil
	}
	if certExists || keyExists {
		return fmt.Errorf("proxy certificate and key must either both exist or both be absent")
	}

	privateKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	if serial.Sign() == 0 {
		serial.SetInt64(1)
	}
	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "tesla-proxy"},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"tesla-proxy", "localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return fmt.Errorf("write proxy TLS key: %w", err)
	}
	if err := os.WriteFile(certificatePath, certPEM, 0644); err != nil {
		return fmt.Errorf("write proxy certificate: %w", err)
	}
	return nil
}
