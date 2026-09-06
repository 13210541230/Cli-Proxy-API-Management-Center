package update

import (
	"archive/zip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExtractZipRejectsTraversal(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "update.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("../outside")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte("should not extract"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	err = ExtractArchive(archivePath, filepath.Join(t.TempDir(), "extracted"))
	if err == nil || !strings.Contains(err.Error(), "unsafe update archive path") {
		t.Fatalf("ExtractArchive() error = %v", err)
	}
}

func TestLocateBundleFindsPlatformExecutables(t *testing.T) {
	root := t.TempDir()
	extension := ""
	if runtime.GOOS == "windows" {
		extension = ".exe"
	}
	for _, name := range []string{"cli-proxy-api", "cpa-manager", "cpa-updater"} {
		path := filepath.Join(root, "nested", name+extension)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(name), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	files, err := LocateBundle(root)
	if err != nil {
		t.Fatalf("LocateBundle() error = %v", err)
	}
	if files.CPAPath == "" || files.ManagerPath == "" || files.UpdaterPath == "" {
		t.Fatalf("LocateBundle() = %#v", files)
	}
}
