package update

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplacePluginDirectoryPreservesConfigAndRollsBack(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "plugins")
	if err := os.MkdirAll(filepath.Join(target, "enterprise"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "enterprise", "plugin.dll"), []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "enterprise", "settings.json"), []byte("user settings"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "enterprise", "retired.txt"), []byte("old file"), 0o644); err != nil {
		t.Fatal(err)
	}

	source := filepath.Join(t.TempDir(), "plugins")
	if err := os.MkdirAll(filepath.Join(source, "enterprise"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "enterprise", "plugin.dll"), []byte("new binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "enterprise", "settings.json"), []byte("package default"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "enterprise", "new.txt"), []byte("new file"), 0o644); err != nil {
		t.Fatal(err)
	}

	backup, err := planBackup(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := replaceDirectoryWithBackup(source, &backup, true); err != nil {
		t.Fatal(err)
	}
	assertFileContents(t, filepath.Join(target, "enterprise", "plugin.dll"), "new binary")
	assertFileContents(t, filepath.Join(target, "enterprise", "settings.json"), "user settings")
	assertFileContents(t, filepath.Join(target, "enterprise", "new.txt"), "new file")
	if _, err := os.Stat(filepath.Join(target, "enterprise", "retired.txt")); !os.IsNotExist(err) {
		t.Fatalf("retired plugin file still exists, stat error = %v", err)
	}

	if err := rollbackPersistedBackups(persistedBackups([]backupFile{backup})); err != nil {
		t.Fatal(err)
	}
	assertFileContents(t, filepath.Join(target, "enterprise", "plugin.dll"), "old binary")
	assertFileContents(t, filepath.Join(target, "enterprise", "settings.json"), "user settings")
	assertFileContents(t, filepath.Join(target, "enterprise", "retired.txt"), "old file")
}

func assertFileContents(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, data, want)
	}
}
