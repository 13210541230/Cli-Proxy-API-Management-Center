package update

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientCheckLatestLoadsAndSelectsManifestAsset(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/" + CanonicalRepository + "/releases/latest":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"tag_name":"v7.2.146","html_url":"https://github.com/%s/releases/tag/v7.2.146","assets":[{"name":"manifest.json","browser_download_url":"%s/manifest.json"},{"name":"CLIProxyAPI-Suite_7.2.146_windows_amd64.zip","browser_download_url":"https://github.com/%s/releases/download/v7.2.146/CLIProxyAPI-Suite_7.2.146_windows_amd64.zip"}]}`, CanonicalRepository, server.URL, CanonicalRepository)
		case "/manifest.json":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"schema":1,"bundleVersion":"7.2.146","cpaVersion":"7.2.146","managerVersion":"1.22.0","assets":[{"os":"windows","arch":"amd64","name":"CLIProxyAPI-Suite_7.2.146_windows_amd64.zip","format":"zip","downloadUrl":"https://github.com/%s/releases/download/v7.2.146/CLIProxyAPI-Suite_7.2.146_windows_amd64.zip","sha256":"%s"}]}`, CanonicalRepository, strings.Repeat("a", 64))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	manifest, err := (&Client{HTTPClient: server.Client(), APIBaseURL: server.URL}).CheckLatest(context.Background())
	if err != nil {
		t.Fatalf("CheckLatest() error = %v", err)
	}
	if manifest.CPAVersion != "7.2.146" || manifest.ManagerVersion != "1.22.0" {
		t.Fatalf("manifest = %#v", manifest)
	}
	asset, err := manifest.AssetFor("windows", "amd64")
	if err != nil {
		t.Fatalf("AssetFor() error = %v", err)
	}
	if asset.Name != "CLIProxyAPI-Suite_7.2.146_windows_amd64.zip" {
		t.Fatalf("asset = %#v", asset)
	}
}

func TestNormalizeManifestUsesReleaseAssetURL(t *testing.T) {
	manifest := Manifest{
		Schema:         ManifestSchema,
		BundleVersion:  "7.2.146",
		ReleaseTag:     "v7.2.146",
		CPAVersion:     "7.2.146",
		ManagerVersion: "1.22.0",
		Assets: []Asset{{
			OS:          "windows",
			Arch:        "amd64",
			Name:        "bundle.zip",
			DownloadURL: "https://example.invalid/redirect.zip",
			SHA256:      strings.Repeat("a", 64),
		}},
	}
	release := releaseResponse{
		TagName: "v7.2.146",
		Assets: []releaseAsset{{
			Name:               "bundle.zip",
			BrowserDownloadURL: "https://github.com/13210541230/CLIProxyAPI/releases/download/v7.2.146/bundle.zip",
		}},
	}
	if err := normalizeAndValidate(&manifest, release, releaseAsset{}); err != nil {
		t.Fatalf("normalizeAndValidate() error = %v", err)
	}
	if manifest.Assets[0].DownloadURL != release.Assets[0].BrowserDownloadURL {
		t.Fatalf("download URL = %q, want %q", manifest.Assets[0].DownloadURL, release.Assets[0].BrowserDownloadURL)
	}
}

func TestNormalizeManifestRejectsExternalReleaseAssetURL(t *testing.T) {
	manifest := Manifest{
		Schema:         ManifestSchema,
		BundleVersion:  "7.2.146",
		ReleaseTag:     "v7.2.146",
		CPAVersion:     "7.2.146",
		ManagerVersion: "1.22.0",
		Assets: []Asset{{
			OS:     "windows",
			Arch:   "amd64",
			Name:   "bundle.zip",
			SHA256: strings.Repeat("a", 64),
		}},
	}
	release := releaseResponse{
		TagName: "v7.2.146",
		Assets: []releaseAsset{{
			Name:               "bundle.zip",
			BrowserDownloadURL: "https://example.invalid/bundle.zip",
		}},
	}
	if err := normalizeAndValidate(&manifest, release, releaseAsset{}); err == nil || !strings.Contains(err.Error(), "outside the canonical repository") {
		t.Fatalf("normalizeAndValidate() error = %v", err)
	}
}

func TestManifestAssetForRejectsUnsupportedTarget(t *testing.T) {
	_, err := (Manifest{Schema: ManifestSchema, Assets: []Asset{{OS: "windows", Arch: "amd64"}}}).AssetFor("linux", "amd64")
	if err == nil || !strings.Contains(err.Error(), "does not support") {
		t.Fatalf("AssetFor() error = %v", err)
	}
}

func TestNormalizeManifestRejectsUnsafeAssetFilename(t *testing.T) {
	manifest := Manifest{
		Schema:         ManifestSchema,
		BundleVersion:  "7.2.146",
		ReleaseTag:     "v7.2.146",
		CPAVersion:     "7.2.146",
		ManagerVersion: "1.22.0",
		Assets: []Asset{{
			OS:     "windows",
			Arch:   "amd64",
			Name:   "../bundle.zip",
			SHA256: strings.Repeat("a", 64),
		}},
	}
	release := releaseResponse{
		TagName: "v7.2.146",
		Assets: []releaseAsset{{
			Name:               "../bundle.zip",
			BrowserDownloadURL: "https://github.com/13210541230/CLIProxyAPI/releases/download/v7.2.146/bundle.zip",
		}},
	}
	if err := normalizeAndValidate(&manifest, release, releaseAsset{}); err == nil || !strings.Contains(err.Error(), "unsafe filename") {
		t.Fatalf("normalizeAndValidate() error = %v", err)
	}
}
