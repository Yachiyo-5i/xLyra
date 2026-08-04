package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMasterKeyCreatesFile(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	key, err := LoadMasterKey(workdir)
	if err != nil {
		t.Fatalf("load master key: %v", err)
	}
	if key == "" {
		t.Fatal("expected generated key")
	}

	path := MasterKeyPath(workdir)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated key: %v", err)
	}
	if string(raw) != key+"\n" {
		t.Fatalf("expected generated key file to match returned key")
	}
	assertFileMode(t, path, masterKeyFileMode)
}

func TestLoadMasterKeyReadsExistingFileAndTightensPermissions(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	path := MasterKeyPath(workdir)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("existing-key\n"), 0644); err != nil {
		t.Fatalf("write existing key: %v", err)
	}

	key, err := LoadMasterKey(workdir)
	if err != nil {
		t.Fatalf("load master key: %v", err)
	}
	if key != "existing-key" {
		t.Fatalf("expected existing key, got %q", key)
	}
	assertFileMode(t, path, masterKeyFileMode)
}

func TestLoadMasterKeyRejectsEmptyFile(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	path := MasterKeyPath(workdir)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("\n"), masterKeyFileMode); err != nil {
		t.Fatalf("write empty key: %v", err)
	}

	if _, err := LoadMasterKey(workdir); err == nil {
		t.Fatal("expected empty master key file to fail")
	}
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("expected %s mode %o, got %o", path, want, got)
	}
}
