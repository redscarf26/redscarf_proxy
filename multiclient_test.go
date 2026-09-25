package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAdditionalClientPortalMatchesNormalStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Config.wtf")
	if err := os.WriteFile(path, []byte("SET portal \"old.server\"\r\nSET gxWindow \"1\"\r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := setPortal(path, bnetPort); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !usesLocalProxyPortal(string(data)) {
		t.Fatal("additional client rejected normal launcher's portal")
	}
	for _, invalid := range []string{"", "SET portal \"remote:7000\"", "SET portal \"127.0.0.1:17000\"", "# SET portal \"127.0.0.1:7000\""} {
		if usesLocalProxyPortal(invalid) {
			t.Fatalf("accepted invalid configuration %q", invalid)
		}
	}
}

func TestAdditionalClientFlag(t *testing.T) {
	if !hasAdditionalClientArg([]string{"--proxy=go", "--additional-client"}) {
		t.Fatal("additional client flag not recognized")
	}
	if hasAdditionalClientArg(nil) || hasAdditionalClientArg([]string{"--proxy=go"}) {
		t.Fatal("normal startup incorrectly selects additional client mode")
	}
}
