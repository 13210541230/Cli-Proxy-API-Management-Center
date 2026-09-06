package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	CanonicalRepository = "13210541230/CLIProxyAPI"
	ManifestAssetName   = "manifest.json"
	ManifestSchema      = 1
	githubAPIBaseURL    = "https://api.github.com"
)

type Manifest struct {
	Schema         int     `json:"schema"`
	BundleVersion  string  `json:"bundleVersion"`
	ReleaseTag     string  `json:"releaseTag"`
	CPAVersion     string  `json:"cpaVersion"`
	ManagerVersion string  `json:"managerVersion"`
	ReleaseURL     string  `json:"releaseUrl,omitempty"`
	Assets         []Asset `json:"assets"`
}

type Asset struct {
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	Name        string `json:"name"`
	Format      string `json:"format"`
	DownloadURL string `json:"downloadUrl"`
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size,omitempty"`
}

type releaseResponse struct {
	TagName     string         `json:"tag_name"`
	HTMLURL     string         `json:"html_url"`
	PublishedAt string         `json:"published_at"`
	Assets      []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	APIURL             string `json:"url"`
}

type Client struct {
	HTTPClient *http.Client
	APIBaseURL string
}

func NewClient() *Client {
	return &Client{HTTPClient: http.DefaultClient, APIBaseURL: githubAPIBaseURL}
}

func (c *Client) CheckLatest(ctx context.Context) (Manifest, error) {
	if c == nil {
		return Manifest{}, errors.New("update client is nil")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(c.APIBaseURL), "/")
	if baseURL == "" {
		baseURL = githubAPIBaseURL
	}
	endpoint := baseURL + "/repos/" + CanonicalRepository + "/releases/latest"
	response, err := c.getJSON(ctx, endpoint, "application/vnd.github+json")
	if err != nil {
		return Manifest{}, fmt.Errorf("fetch latest CLIProxyAPI release: %w", err)
	}

	var release releaseResponse
	if err := json.Unmarshal(response, &release); err != nil {
		return Manifest{}, fmt.Errorf("decode latest release: %w", err)
	}
	var manifestAsset releaseAsset
	for _, asset := range release.Assets {
		if strings.EqualFold(strings.TrimSpace(asset.Name), ManifestAssetName) {
			manifestAsset = asset
			break
		}
	}
	if manifestAsset.Name == "" {
		return Manifest{}, errors.New("latest release does not contain manifest.json")
	}
	manifestURL := strings.TrimSpace(manifestAsset.BrowserDownloadURL)
	if manifestURL == "" {
		manifestURL = strings.TrimSpace(manifestAsset.APIURL)
	}
	if manifestURL == "" {
		return Manifest{}, errors.New("manifest.json has no download URL")
	}
	manifestData, err := c.getJSON(ctx, manifestURL, "application/octet-stream")
	if err != nil {
		return Manifest{}, fmt.Errorf("fetch release manifest: %w", err)
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode release manifest: %w", err)
	}
	if err := normalizeAndValidate(&manifest, release, manifestAsset); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (m Manifest) AssetFor(goos, goarch string) (Asset, error) {
	goos = strings.ToLower(strings.TrimSpace(goos))
	goarch = strings.ToLower(strings.TrimSpace(goarch))
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	for _, asset := range m.Assets {
		if strings.EqualFold(asset.OS, goos) && strings.EqualFold(asset.Arch, goarch) {
			return asset, nil
		}
	}
	return Asset{}, fmt.Errorf("latest bundle does not support %s/%s", goos, goarch)
}

func (c *Client) getJSON(ctx context.Context, endpoint, accept string) ([]byte, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid update URL %q", endpoint)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", accept)
	request.Header.Set("User-Agent", "CLIProxyAPI-Manager/update")
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("GitHub returned %s", response.Status)
	}
	var data json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&data); err != nil {
		return nil, err
	}
	return data, nil
}

func normalizeAndValidate(manifest *Manifest, release releaseResponse, manifestAsset releaseAsset) error {
	if manifest.Schema != ManifestSchema {
		return fmt.Errorf("unsupported release manifest schema %d", manifest.Schema)
	}
	manifest.BundleVersion = strings.TrimSpace(manifest.BundleVersion)
	manifest.ReleaseTag = strings.TrimSpace(manifest.ReleaseTag)
	manifest.CPAVersion = strings.TrimSpace(manifest.CPAVersion)
	manifest.ManagerVersion = strings.TrimSpace(manifest.ManagerVersion)
	if manifest.BundleVersion == "" || manifest.CPAVersion == "" || manifest.ManagerVersion == "" {
		return errors.New("release manifest is missing version fields")
	}
	if manifest.ReleaseTag == "" {
		manifest.ReleaseTag = strings.TrimSpace(release.TagName)
	} else if release.TagName != "" && !strings.EqualFold(manifest.ReleaseTag, strings.TrimSpace(release.TagName)) {
		return fmt.Errorf("release manifest tag %s does not match latest release %s", manifest.ReleaseTag, release.TagName)
	}
	if manifest.ReleaseURL == "" {
		manifest.ReleaseURL = strings.TrimSpace(release.HTMLURL)
	}
	if len(manifest.Assets) == 0 {
		return errors.New("release manifest contains no assets")
	}
	byName := make(map[string]releaseAsset, len(release.Assets))
	for _, asset := range release.Assets {
		byName[strings.ToLower(strings.TrimSpace(asset.Name))] = asset
	}
	seen := make(map[string]struct{}, len(manifest.Assets))
	for i := range manifest.Assets {
		asset := &manifest.Assets[i]
		asset.OS = strings.ToLower(strings.TrimSpace(asset.OS))
		asset.Arch = strings.ToLower(strings.TrimSpace(asset.Arch))
		asset.Name = strings.TrimSpace(asset.Name)
		asset.Format = strings.ToLower(strings.TrimSpace(asset.Format))
		asset.DownloadURL = strings.TrimSpace(asset.DownloadURL)
		asset.SHA256 = strings.ToLower(strings.TrimSpace(asset.SHA256))
		if asset.OS == "" || asset.Arch == "" || asset.Name == "" || asset.SHA256 == "" {
			return fmt.Errorf("release manifest asset %d is incomplete", i)
		}
		if asset.Name == "." || filepath.Base(filepath.Clean(asset.Name)) != asset.Name || strings.ContainsAny(asset.Name, `/\\`) {
			return fmt.Errorf("release manifest asset %s has an unsafe filename", asset.Name)
		}
		if len(asset.SHA256) != 64 {
			return fmt.Errorf("release manifest asset %s has an invalid SHA-256", asset.Name)
		}
		if _, ok := seen[strings.ToLower(asset.Name)]; ok {
			return fmt.Errorf("release manifest contains duplicate asset %s", asset.Name)
		}
		seen[strings.ToLower(asset.Name)] = struct{}{}
		releaseAsset, ok := byName[strings.ToLower(asset.Name)]
		if !ok {
			return fmt.Errorf("release manifest asset %s is not attached to the release", asset.Name)
		}
		// Always use the URL returned for the matching asset in this release.
		// The manifest is metadata; it must not redirect downloads elsewhere.
		asset.DownloadURL = strings.TrimSpace(releaseAsset.BrowserDownloadURL)
		if asset.DownloadURL == "" {
			asset.DownloadURL = strings.TrimSpace(releaseAsset.APIURL)
		}
		if err := validateReleaseDownloadURL(asset.DownloadURL); err != nil {
			return fmt.Errorf("release manifest asset %s: %w", asset.Name, err)
		}
	}
	_ = manifestAsset
	return nil
}
