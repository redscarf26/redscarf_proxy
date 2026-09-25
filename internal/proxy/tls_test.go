package proxy

import (
	"crypto/x509"
	"testing"
	"time"
)

func TestDefaultTLSUsesAuroraCompatibilityChain(t *testing.T) {
	config, development, err := loadTLSConfig("", "")
	if err != nil {
		t.Fatal(err)
	}
	if development {
		t.Fatal("default TLS unexpectedly uses a development certificate")
	}
	if len(config.Certificates) != 1 || len(config.Certificates[0].Certificate) != 3 {
		t.Fatalf("unexpected certificate chain length: %#v", config.Certificates)
	}
	leaf, err := x509.ParseCertificate(config.Certificates[0].Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if leaf.Subject.CommonName != "*.*" {
		t.Fatalf("leaf common name %q", leaf.Subject.CommonName)
	}
	if !leaf.NotAfter.After(time.Now()) {
		t.Fatalf("Aurora compatibility certificate expired at %s", leaf.NotAfter)
	}
}

func TestBNETTLSOmitsHTTPALPN(t *testing.T) {
	config, _, err := loadTLSConfig("", "")
	if err != nil {
		t.Fatal(err)
	}
	if protos := bnetTLSConfig(config).NextProtos; len(protos) != 0 {
		t.Fatalf("BNet ALPN %v, want none", protos)
	}
	if protos := restTLSConfig(config).NextProtos; len(protos) != 1 || protos[0] != "http/1.1" {
		t.Fatalf("REST ALPN %v", protos)
	}
}
