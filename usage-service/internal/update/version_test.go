package update

import (
	"path/filepath"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		left  string
		right string
		want  int
		valid bool
	}{
		{left: "v7.3.10", right: "7.3.9", want: 1, valid: true},
		{left: "1.24", right: "1.24.0", want: 0, valid: true},
		{left: "7.3.35-rc.2", right: "7.3.35-rc.10", want: -1, valid: true},
		{left: "7.3.35", right: "7.3.35-rc.10", want: 1, valid: true},
		{left: "dev", right: "1.0.0", want: 0, valid: false},
	}
	for _, tc := range cases {
		got, valid := CompareVersions(tc.left, tc.right)
		if got != tc.want || valid != tc.valid {
			t.Errorf("CompareVersions(%q, %q) = (%d, %v), want (%d, %v)", tc.left, tc.right, got, valid, tc.want, tc.valid)
		}
	}
}

func TestSuiteVersionMetadataRoundTrip(t *testing.T) {
	root := t.TempDir()
	want := SuiteVersion{ReleaseTag: "v7.3.35", CPAVersion: "7.3.35", ManagerVersion: "1.24.7"}
	if err := WriteSuiteVersion(root, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadSuiteVersion(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Schema != 1 || got.CPAVersion != want.CPAVersion || got.ManagerVersion != want.ManagerVersion {
		t.Fatalf("ReadSuiteVersion() = %#v, want %#v", got, want)
	}
}

func TestHasNewerSuiteVersion(t *testing.T) {
	current := SuiteVersion{CPAVersion: "7.3.34", ManagerVersion: "1.24.7"}
	latest := Manifest{CPAVersion: "7.3.35", ManagerVersion: "1.24.7"}
	available, err := HasNewerSuiteVersion(latest, current)
	if err != nil || !available {
		t.Fatalf("HasNewerSuiteVersion() = (%v, %v), want (true, nil)", available, err)
	}
	latest.CPAVersion = current.CPAVersion
	available, err = HasNewerSuiteVersion(latest, current)
	if err != nil || available {
		t.Fatalf("HasNewerSuiteVersion() = (%v, %v), want (false, nil)", available, err)
	}
}

func TestSuiteRuntimeStatePath(t *testing.T) {
	root := t.TempDir()
	if got := SuiteRuntimeStatePath(root); got != filepath.Join(root, ".suite-runtime.json") {
		t.Fatalf("SuiteRuntimeStatePath() = %q", got)
	}
}
