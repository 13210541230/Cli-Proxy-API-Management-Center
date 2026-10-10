package update

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLocateBundleResolvesTopLevelPackageRoot(t *testing.T) {
	archiveRoot := t.TempDir()
	packageRoot := filepath.Join(archiveRoot, "CLIProxyAPI-Suite_7.3.35_windows_amd64")
	if runtime.GOOS != "windows" {
		packageRoot = filepath.Join(archiveRoot, "CLIProxyAPI-Suite_7.3.35_linux_amd64")
	}
	if err := os.MkdirAll(packageRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	extension := ""
	if runtime.GOOS == "windows" {
		extension = ".exe"
	}
	for _, name := range []string{"cli-proxy-api", "cpa-manager", "cpa-updater"} {
		if err := os.WriteFile(filepath.Join(packageRoot, name+extension), []byte(name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(packageRoot, suiteVersionFilename), []byte(`{"schema":1,"cpaVersion":"7.3.35","managerVersion":"1.24.7"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageRoot, "README.md"), []byte("release docs"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageRoot, "SUITE-README.md"), []byte("launcher and update guide"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(packageRoot, "plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(packageRoot, "static"), 0o755); err != nil {
		t.Fatal(err)
	}

	bundle, err := LocateBundle(archiveRoot)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.RootPath != packageRoot || bundle.VersionPath != filepath.Join(packageRoot, suiteVersionFilename) {
		t.Fatalf("package root/version = %q/%q", bundle.RootPath, bundle.VersionPath)
	}
	if bundle.Files["README.md"] != filepath.Join(packageRoot, "README.md") || bundle.Files["SUITE-README.md"] != filepath.Join(packageRoot, "SUITE-README.md") || bundle.PluginsPath != filepath.Join(packageRoot, "plugins") || bundle.StaticPath != filepath.Join(packageRoot, "static") {
		t.Fatalf("package assets = %#v", bundle)
	}
}
