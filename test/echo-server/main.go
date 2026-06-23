// echo-server is a minimal HTTPS server that echoes request metadata as JSON.
// When TLS_CERT_FILE and TLS_KEY_FILE point to existing files (e.g. from an
// OpenShift service-serving cert Secret), those are used. Otherwise a
// self-signed certificate is generated at startup.
//
// Environment variables:
//   - PORT:          listen port (default "8443")
//   - TLS_CERT_FILE: path to PEM certificate (default "/tls/tls.crt")
//   - TLS_KEY_FILE:  path to PEM private key  (default "/tls/tls.key")
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"time"
)

type echoResponse struct {
	Method  string              `json:"method"`
	Path    string              `json:"path"`
	Headers map[string][]string `json:"headers"`
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8443"
	}
	certFile := os.Getenv("TLS_CERT_FILE")
	if certFile == "" {
		certFile = "/tls/tls.crt"
	}
	keyFile := os.Getenv("TLS_KEY_FILE")
	if keyFile == "" {
		keyFile = "/tls/tls.key"
	}

	tlsCert, certSource, err := loadOrGenerateCert(certFile, keyFile)
	if err != nil {
		log.Fatalf("failed to obtain TLS cert: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		resp := echoResponse{
			Method:  r.Method,
			Path:    r.URL.Path,
			Headers: r.Header,
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("encode error: %v", err)
		}
	})

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{tlsCert},
			MinVersion:   tls.VersionTLS12,
		},
	}

	log.Printf("echo-server listening on :%s (HTTPS, cert: %s)", port, certSource)
	if err := srv.ListenAndServeTLS("", ""); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func loadOrGenerateCert(certFile, keyFile string) (tls.Certificate, string, error) {
	if _, err := os.Stat(certFile); err == nil {
		if _, err := os.Stat(keyFile); err == nil {
			cert, err := tls.LoadX509KeyPair(certFile, keyFile)
			if err != nil {
				return tls.Certificate{}, "", fmt.Errorf("load %s / %s: %w", certFile, keyFile, err)
			}
			return cert, certFile, nil
		}
	}
	cert, err := generateSelfSignedCert()
	if err != nil {
		return tls.Certificate{}, "", err
	}
	return cert, "self-signed", nil
}

func generateSelfSignedCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate serial: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "echo-server"},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames: []string{
			"localhost",
			"*.konflux-kite.svc.cluster.local",
			"*.kubearchive.svc.cluster.local",
			"*.test-echo-watson.svc.cluster.local",
		},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("create cert: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("marshal key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return tls.X509KeyPair(certPEM, keyPEM)
}
