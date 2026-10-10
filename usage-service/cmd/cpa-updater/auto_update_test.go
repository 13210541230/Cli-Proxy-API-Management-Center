package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalHealthEndpoint(t *testing.T) {
	cases := []struct {
		address  string
		path     string
		fallback string
		want     string
	}{
		{address: "0.0.0.0:18317", path: "/health", fallback: "fallback", want: "http://127.0.0.1:18317/health"},
		{address: ":9000", path: "/health", fallback: "fallback", want: "http://127.0.0.1:9000/health"},
		{address: "http://localhost:18317/custom", path: "/health", fallback: "fallback", want: "http://localhost:18317/health"},
		{address: "invalid", path: "/health", fallback: "fallback", want: "fallback"},
	}
	for _, tc := range cases {
		if got := localHealthEndpoint(tc.address, tc.path, tc.fallback); got != tc.want {
			t.Errorf("localHealthEndpoint(%q) = %q, want %q", tc.address, got, tc.want)
		}
	}
}

func TestResolveHealthURLsUsesSelectedCPAConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "custom.yaml")
	if err := os.WriteFile(configPath, []byte("host: \"\"\nport: 9001 # custom port\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	managerURL, cpaURL, err := resolveHealthURLs(managerSettings{HTTPAddr: "0.0.0.0:18317"}, configPath)
	if err != nil {
		t.Fatal(err)
	}
	if managerURL != "http://127.0.0.1:18317/health" || cpaURL != "http://127.0.0.1:9001/healthz" {
		t.Fatalf("resolveHealthURLs() = (%q, %q)", managerURL, cpaURL)
	}
}

func TestCPAHealthURLFromConfigSupportsHostAndPort(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte("host: '::1'\nport: 8319\ntls:\n  enable: true\n  port: 9999\n")
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := cpaHealthURLFromConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://[::1]:8319/healthz" {
		t.Fatalf("cpaHealthURLFromConfig() = %q", got)
	}
}
