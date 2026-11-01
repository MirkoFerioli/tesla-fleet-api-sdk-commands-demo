package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnsureCertificateGeneratesProxyTLSPair(t *testing.T) {
	directory := t.TempDir()
	certificatePath := filepath.Join(directory, "tls-cert.pem")
	keyPath := filepath.Join(directory, "tls-key.pem")
	now := time.Now()
	if err := ensureCertificate(certificatePath, keyPath, now); err != nil {
		t.Fatal(err)
	}
	if _, err := tls.LoadX509KeyPair(certificatePath, keyPath); err != nil {
		t.Fatal(err)
	}
	certInfo, err := os.Stat(certificatePath)
	if err != nil || certInfo.Mode().Perm() != 0644 {
		t.Fatalf("unexpected certificate permissions: %v", err)
	}
	keyInfo, err := os.Stat(keyPath)
	if err != nil || keyInfo.Mode().Perm() != 0600 {
		t.Fatalf("unexpected key permissions: %v", err)
	}
	encoded, err := os.ReadFile(certificatePath)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(encoded)
	if block == nil {
		t.Fatal("certificate is not PEM encoded")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if certificate.NotAfter.Sub(certificate.NotBefore) < 3650*24*time.Hour {
		t.Fatal("certificate validity is shorter than ten years")
	}
	if err := certificate.VerifyHostname("tesla-proxy"); err != nil {
		t.Fatal(err)
	}
	if err := certificate.VerifyHostname("localhost"); err != nil {
		t.Fatal(err)
	}
	if err := certificate.VerifyHostname("127.0.0.1"); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureCertificateRejectsPartialPairAndPreservesExistingPair(t *testing.T) {
	directory := t.TempDir()
	certificatePath := filepath.Join(directory, "tls-cert.pem")
	keyPath := filepath.Join(directory, "tls-key.pem")
	if err := os.WriteFile(certificatePath, []byte("certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ensureCertificate(certificatePath, keyPath, time.Now()); err == nil {
		t.Fatal("accepted incomplete certificate pair")
	}
	if err := os.Remove(certificatePath); err != nil {
		t.Fatal(err)
	}
	if err := ensureCertificate(certificatePath, keyPath, time.Now()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(certificatePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureCertificate(certificatePath, keyPath, time.Now()); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(certificatePath)
	if err != nil || string(before) != string(after) {
		t.Fatal("existing certificate was replaced")
	}
}

func TestFlagValueSupportsSeparatedAndEqualsForms(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{args: []string{"-cert", "/a/cert.pem"}, want: "/a/cert.pem"},
		{args: []string{"-cert=/b/cert.pem"}, want: "/b/cert.pem"},
		{args: []string{"-port", "4443"}, want: "fallback"},
	} {
		if got := flagValue(test.args, "-cert", "fallback"); got != test.want {
			t.Fatalf("flagValue(%v) = %q, want %q", test.args, got, test.want)
		}
	}
}
