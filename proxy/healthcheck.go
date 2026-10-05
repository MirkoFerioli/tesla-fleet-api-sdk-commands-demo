package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	if err := checkHealth(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func checkHealth() error {
	certificate, err := os.ReadFile("/config/tls-cert.pem")
	if err != nil {
		return err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certificate) {
		return fmt.Errorf("invalid proxy TLS certificate")
	}
	client := &http.Client{
		Timeout: 4 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
			VerifyConnection: func(connection tls.ConnectionState) error {
				intermediates := x509.NewCertPool()
				for _, cert := range connection.PeerCertificates[1:] {
					intermediates.AddCert(cert)
				}
				_, err := connection.PeerCertificates[0].Verify(x509.VerifyOptions{
					Roots: roots, Intermediates: intermediates,
				})
				return err
			},
		}},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	response, err := client.Get("https://127.0.0.1:4443/health")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("proxy health returned HTTP %d", response.StatusCode)
	}
	return nil
}
