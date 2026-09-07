package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const maxUpdateAssetSize int64 = 2 << 30

type BundleFiles struct {
	CPAPath     string
	ManagerPath string
	UpdaterPath string
}

func (c *Client) DownloadAsset(ctx context.Context, asset Asset, destination string) error {
	if err := validateReleaseDownloadURL(asset.DownloadURL); err != nil {
		return err
	}
	if !isSHA256(asset.SHA256) {
		return fmt.Errorf("release asset %s has an invalid SHA-256", asset.Name)
	}
	if strings.TrimSpace(destination) == "" {
		return errors.New("update destination is empty")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("create update directory: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.DownloadURL, nil)
	if err != nil {
		return fmt.Errorf("create update download request: %w", err)
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("User-Agent", "CLIProxyAPI-Manager/update")
	if token := strings.TrimSpace(c.GitHubToken); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("download update asset: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("download update asset: GitHub returned %s", response.Status)
	}
	if response.ContentLength > maxUpdateAssetSize {
		return fmt.Errorf("update asset %s exceeds the size limit", asset.Name)
	}

	temporary := destination + ".part"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create update asset: %w", err)
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, maxUpdateAssetSize+1))
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("write update asset: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("close update asset: %w", closeErr)
	}
	if written > maxUpdateAssetSize {
		_ = os.Remove(temporary)
		return fmt.Errorf("update asset %s exceeds the size limit", asset.Name)
	}
	if actual := hex.EncodeToString(hash.Sum(nil)); !strings.EqualFold(actual, asset.SHA256) {
		_ = os.Remove(temporary)
		return fmt.Errorf("update asset %s checksum mismatch: got %s", asset.Name, actual)
	}
	if err := os.Rename(temporary, destination); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("commit downloaded update asset: %w", err)
	}
	return nil
}

func ExtractArchive(archivePath, destination string) error {
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return fmt.Errorf("create extraction directory: %w", err)
	}
	lower := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(archivePath, destination)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return extractTarGz(archivePath, destination)
	default:
		return fmt.Errorf("unsupported update archive format: %s", archivePath)
	}
}

func LocateBundle(root string) (BundleFiles, error) {
	managerName := "cpa-manager"
	cpaName := "cli-proxy-api"
	updaterName := "cpa-updater"
	if runtime.GOOS == "windows" {
		managerName += ".exe"
		cpaName += ".exe"
		updaterName += ".exe"
	}

	var bundle BundleFiles
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		switch strings.ToLower(entry.Name()) {
		case strings.ToLower(cpaName):
			bundle.CPAPath = path
		case strings.ToLower(managerName):
			bundle.ManagerPath = path
		case strings.ToLower(updaterName):
			bundle.UpdaterPath = path
		}
		return nil
	})
	if err != nil {
		return BundleFiles{}, fmt.Errorf("scan extracted update: %w", err)
	}
	if bundle.CPAPath == "" || bundle.ManagerPath == "" || bundle.UpdaterPath == "" {
		return BundleFiles{}, errors.New("update bundle must contain cli-proxy-api, cpa-manager, and cpa-updater")
	}
	return bundle, nil
}

func extractZip(archivePath, destination string) error {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open update zip: %w", err)
	}
	defer archive.Close()
	for _, entry := range archive.File {
		path, err := safeArchivePath(destination, entry.Name)
		if err != nil {
			return err
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("update archive contains unsupported symlink: %s", entry.Name)
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(path, 0o755); err != nil {
				return fmt.Errorf("create extracted directory: %w", err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create extracted parent: %w", err)
		}
		reader, err := entry.Open()
		if err != nil {
			return fmt.Errorf("open archived file %s: %w", entry.Name, err)
		}
		mode := entry.Mode() & 0o777
		if mode == 0 {
			mode = 0o644
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
		if err == nil {
			_, err = io.Copy(file, reader)
			if closeErr := file.Close(); err == nil {
				err = closeErr
			}
		}
		closeErr := reader.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return fmt.Errorf("extract archived file %s: %w", entry.Name, err)
		}
	}
	return nil
}

func extractTarGz(archivePath, destination string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open update archive: %w", err)
	}
	defer file.Close()
	gzipReader, err := newGzipReader(file)
	if err != nil {
		return fmt.Errorf("open update gzip: %w", err)
	}
	defer gzipReader.Close()

	reader := tar.NewReader(gzipReader)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read update archive: %w", err)
		}
		path, err := safeArchivePath(destination, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o755); err != nil {
				return fmt.Errorf("create extracted directory: %w", err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return fmt.Errorf("create extracted parent: %w", err)
			}
			mode := os.FileMode(header.Mode) & 0o777
			if mode == 0 {
				mode = 0o644
			}
			file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
			if err != nil {
				return fmt.Errorf("create extracted file %s: %w", header.Name, err)
			}
			_, copyErr := io.Copy(file, reader)
			closeErr := file.Close()
			if copyErr != nil {
				return fmt.Errorf("extract file %s: %w", header.Name, copyErr)
			}
			if closeErr != nil {
				return fmt.Errorf("close extracted file %s: %w", header.Name, closeErr)
			}
		default:
			return fmt.Errorf("update archive contains unsupported entry %s", header.Name)
		}
	}
}

func safeArchivePath(destination, name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe update archive path: %s", name)
	}
	root, err := filepath.Abs(destination)
	if err != nil {
		return "", err
	}
	path, err := filepath.Abs(filepath.Join(root, clean))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("unsafe update archive path: %s", name)
	}
	return path, nil
}

func validateReleaseDownloadURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" {
		return fmt.Errorf("update asset URL must use HTTPS: %q", raw)
	}
	host := strings.ToLower(parsed.Hostname())
	path := strings.ToLower(parsed.Path)
	if host == "github.com" && strings.HasPrefix(path, "/"+strings.ToLower(CanonicalRepository)+"/releases/download/") {
		return nil
	}
	if host == "api.github.com" && strings.HasPrefix(path, "/repos/"+strings.ToLower(CanonicalRepository)+"/releases/assets/") {
		return nil
	}
	return fmt.Errorf("update asset URL is outside the canonical repository: %q", raw)
}

func validateReleaseDownloadURLForTag(raw, tag string) error {
	if err := validateReleaseDownloadURL(raw); err != nil {
		return err
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return err
	}
	if strings.ToLower(parsed.Hostname()) != "github.com" {
		return fmt.Errorf("direct release asset URL must use github.com: %q", raw)
	}
	prefix := "/" + strings.ToLower(CanonicalRepository) + "/releases/download/" + strings.ToLower(url.PathEscape(strings.TrimSpace(tag))) + "/"
	if !strings.HasPrefix(strings.ToLower(parsed.Path), prefix) {
		return fmt.Errorf("direct release asset URL does not match release %s: %q", tag, raw)
	}
	return nil
}

func isSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func newGzipReader(reader io.Reader) (*gzip.Reader, error) {
	return gzip.NewReader(reader)
}
