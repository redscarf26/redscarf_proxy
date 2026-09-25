package proxy

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"time"
)

func loadTLSConfig(certFile, keyFile string) (*tls.Config, bool, error) {
	if certFile != "" {
		certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, false, fmt.Errorf("load BNet TLS certificate: %w", err)
		}
		return secureTLSConfig(certificate), false, nil
	}
	certificate, err := tls.X509KeyPair([]byte(auroraCertificateChainPEM), []byte(auroraPrivateKeyPEM))
	if err != nil {
		return nil, false, fmt.Errorf("load embedded Aurora compatibility certificate: %w", err)
	}
	return secureTLSConfig(certificate), false, nil
}

func secureTLSConfig(certificate tls.Certificate) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{certificate},
		MinVersion:   tls.VersionTLS12,
	}
}

func restTLSConfig(base *tls.Config) *tls.Config {
	config := base.Clone()
	config.NextProtos = []string{"http/1.1"}
	return config
}

func bnetTLSConfig(base *tls.Config) *tls.Config {
	config := base.Clone()
	config.NextProtos = nil
	return config
}

// developmentCertificate remains available to unit tests and diagnostics that
// explicitly need a short-lived certificate. Live defaults use Aurora above.
func developmentCertificate() (tls.Certificate, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate development TLS key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate development TLS serial: %w", err)
	}
	now := time.Now()
	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "redscarf development BNet"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("create development TLS certificate: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, err
	}
	return certificate, nil
}

func ensureTLSFilesReadable(config Config) error {
	for _, path := range []string{config.TLSCertFile, config.TLSKeyFile} {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			return err
		}
	}
	return nil
}
