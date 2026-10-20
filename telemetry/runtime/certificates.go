package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func validateBundle(directory, hostname string, trust []byte) (*x509.Certificate, error) {
	pair, err := tls.LoadX509KeyPair(filepath.Join(directory, "fullchain.pem"), filepath.Join(directory, "server-key.pem"))
	if err != nil {
		return nil, fmt.Errorf("certificate/key pair: %w", err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(trust) {
		return nil, fmt.Errorf("empty or invalid CA bundle")
	}
	intermediates := x509.NewCertPool()
	for _, encoded := range pair.Certificate[1:] {
		certificate, err := x509.ParseCertificate(encoded)
		if err != nil {
			return nil, err
		}
		intermediates.AddCert(certificate)
	}
	_, err = leaf.Verify(x509.VerifyOptions{DNSName: hostname, Roots: roots, Intermediates: intermediates})
	return leaf, err
}

func certificateAuthorities(chain, roots []byte) ([]byte, error) {
	var authorities []byte
	for _, bundle := range [][]byte{chain, roots} {
		for len(bytes.TrimSpace(bundle)) > 0 {
			block, rest := pem.Decode(bundle)
			if block == nil || block.Type != "CERTIFICATE" {
				return nil, fmt.Errorf("invalid PEM certificate bundle")
			}
			certificate, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, err
			}
			if certificate.IsCA {
				authorities = append(authorities, pem.EncodeToMemory(block)...)
			}
			bundle = rest
		}
	}
	if len(authorities) == 0 {
		return nil, fmt.Errorf("no CA certificates found")
	}
	return authorities, nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".pending-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(mode); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), path)
}

func publishBundle(directory, candidate, hostname string, uid, gid int) (bool, error) {
	trust, err := os.ReadFile(filepath.Join(candidate, "ca.pem"))
	if err != nil {
		return false, err
	}
	if _, err := validateBundle(candidate, hostname, trust); err != nil {
		return false, err
	}
	current := filepath.Join(directory, "current")
	previousTrust, err := os.ReadFile(filepath.Join(current, "ca.pem"))
	if err == nil {
		if _, err := validateBundle(candidate, hostname, previousTrust); err != nil {
			return false, fmt.Errorf("CA update required: candidate is not trusted by the CA already configured for the vehicle")
		}
		trust = previousTrust
	} else if !os.IsNotExist(err) {
		return false, err
	}
	chain, err := os.ReadFile(filepath.Join(candidate, "fullchain.pem"))
	if err != nil {
		return false, err
	}
	key, err := os.ReadFile(filepath.Join(candidate, "server-key.pem"))
	if err != nil {
		return false, err
	}
	oldChain, _ := os.ReadFile(filepath.Join(current, "fullchain.pem"))
	oldKey, _ := os.ReadFile(filepath.Join(current, "server-key.pem"))
	if bytes.Equal(chain, oldChain) && bytes.Equal(key, oldKey) {
		return false, nil
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return false, err
	}
	release, err := os.MkdirTemp(directory, "release-")
	if err != nil {
		return false, err
	}
	committed := false
	defer func() {
		if !committed {
			os.RemoveAll(release)
		}
	}()
	for name, content := range map[string][]byte{"fullchain.pem": chain, "server-key.pem": key, "ca.pem": trust, "hostname": []byte(hostname)} {
		path := filepath.Join(release, name)
		if err := os.WriteFile(path, content, 0600); err != nil {
			return false, err
		}
		if err := os.Chown(path, uid, gid); err != nil {
			return false, err
		}
	}
	if err := os.Chown(release, uid, gid); err != nil {
		return false, err
	}
	link := filepath.Join(directory, ".current-next")
	os.Remove(link)
	if err := os.Symlink(filepath.Base(release), link); err != nil {
		return false, err
	}
	if err := os.Rename(link, current); err != nil {
		os.Remove(link)
		return false, err
	}
	committed = true
	return true, nil
}

func bundleFingerprint(directory string) (string, error) {
	chain, err := os.ReadFile(filepath.Join(directory, "fullchain.pem"))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(chain)), nil
}

func validFor(certificate *x509.Certificate, duration time.Duration) bool {
	return time.Until(certificate.NotAfter) > duration
}
