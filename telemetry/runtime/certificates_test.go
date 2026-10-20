package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testBundle(t *testing.T, parent *x509.Certificate, parentKey *ecdsa.PrivateKey, hostname string, lifetimes ...time.Duration) (string, *x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	if parent == nil {
		parentKey, _ = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		parent = &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Test CA"}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(90 * 24 * time.Hour)}
		encoded, err := x509.CreateCertificate(rand.Reader, parent, parent, &parentKey.PublicKey, parentKey)
		if err != nil {
			t.Fatal(err)
		}
		parent, _ = x509.ParseCertificate(encoded)
	}
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	lifetime := 30 * 24 * time.Hour
	if len(lifetimes) != 0 {
		lifetime = lifetimes[0]
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), DNSNames: []string{hostname}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(lifetime)}
	encoded, err := x509.CreateCertificate(rand.Reader, leaf, parent, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatal(err)
	}
	private, _ := x509.MarshalECPrivateKey(key)
	directory := t.TempDir()
	for name, block := range map[string]*pem.Block{"fullchain.pem": {Type: "CERTIFICATE", Bytes: encoded}, "server-key.pem": {Type: "EC PRIVATE KEY", Bytes: private}, "ca.pem": {Type: "CERTIFICATE", Bytes: parent.Raw}} {
		if err := os.WriteFile(filepath.Join(directory, name), pem.EncodeToMemory(block), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return directory, parent, parentKey
}

func TestPublishPreservesValidGeneration(t *testing.T) {
	directory := t.TempDir()
	hostname := "receiver.tailnet.ts.net"
	first, authority, key := testBundle(t, nil, nil, hostname)
	changed, err := publishBundle(directory, first, hostname, os.Getuid(), os.Getgid())
	if err != nil || !changed {
		t.Fatalf("first publication: %v %v", changed, err)
	}
	previous, _ := os.Readlink(filepath.Join(directory, "current"))
	changed, err = publishBundle(directory, first, hostname, os.Getuid(), os.Getgid())
	if err != nil || changed {
		t.Fatalf("unchanged publication: %v %v", changed, err)
	}
	wrongHost, _, _ := testBundle(t, authority, key, "wrong.tailnet.ts.net")
	if _, err := publishBundle(directory, wrongHost, hostname, os.Getuid(), os.Getgid()); err == nil {
		t.Fatal("accepted wrong hostname")
	}
	otherCA, _, _ := testBundle(t, nil, nil, hostname)
	if _, err := publishBundle(directory, otherCA, hostname, os.Getuid(), os.Getgid()); err == nil || !strings.Contains(err.Error(), "CA update required") {
		t.Fatalf("accepted CA change: %v", err)
	}
	active, _ := os.Readlink(filepath.Join(directory, "current"))
	if active != previous {
		t.Fatal("failed renewal changed active generation")
	}
	renewed, _, _ := testBundle(t, authority, key, hostname)
	changed, err = publishBundle(directory, renewed, hostname, os.Getuid(), os.Getgid())
	if err != nil || !changed {
		t.Fatalf("renewal: %v %v", changed, err)
	}
	if _, err := os.Stat(filepath.Join(directory, previous, "server-key.pem")); err != nil {
		t.Fatal("previous generation not preserved")
	}
	info, _ := os.Stat(filepath.Join(directory, "current", "server-key.pem"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("private key permissions")
	}
}

func TestMismatchedKeyAndInvalidCA(t *testing.T) {
	expired, _, _ := testBundle(t, nil, nil, "receiver.tailnet.ts.net", -time.Minute)
	expiredTrust, _ := os.ReadFile(filepath.Join(expired, "ca.pem"))
	if _, err := validateBundle(expired, "receiver.tailnet.ts.net", expiredTrust); err == nil {
		t.Fatal("accepted expired certificate")
	}
	first, _, _ := testBundle(t, nil, nil, "receiver.tailnet.ts.net")
	second, _, _ := testBundle(t, nil, nil, "receiver.tailnet.ts.net")
	key, _ := os.ReadFile(filepath.Join(second, "server-key.pem"))
	os.WriteFile(filepath.Join(first, "server-key.pem"), key, 0600)
	trust, _ := os.ReadFile(filepath.Join(first, "ca.pem"))
	if _, err := validateBundle(first, "receiver.tailnet.ts.net", trust); err == nil {
		t.Fatal("accepted mismatched private key")
	}
	if _, err := certificateAuthorities([]byte("invalid"), trust); err == nil {
		t.Fatal("accepted malformed chain")
	}
	chain, _ := os.ReadFile(filepath.Join(second, "fullchain.pem"))
	authorities, err := certificateAuthorities(chain, trust)
	if err != nil {
		t.Fatal(err)
	}
	block, rest := pem.Decode(authorities)
	if block == nil || len(rest) != 0 {
		t.Fatal("leaf included in CA bundle")
	}
}
