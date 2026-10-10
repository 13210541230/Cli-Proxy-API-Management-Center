package update

import (
	"net/http"
	"testing"
)

func TestHealthHTTPClientAllowsSelfSignedTLSOnlyOnLoopback(t *testing.T) {
	client := healthHTTPClient("https://127.0.0.1:8317/healthz")
	if client == http.DefaultClient {
		t.Fatal("loopback HTTPS should use a local health probe client")
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil || !transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("loopback HTTPS client should allow a self-signed local certificate")
	}
	if got := healthHTTPClient("https://example.com/healthz"); got != http.DefaultClient {
		t.Fatal("remote HTTPS health checks must retain certificate verification")
	}
}
