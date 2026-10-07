package builtingen

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// TestGeneratedBuiltinsAreCurrent fails when src/plugin.js or _shared changed
// without running go generate ./internal/jsplugin.
func TestGeneratedBuiltinsAreCurrent(t *testing.T) {
	source := filepath.Join("..", "builtin")
	scratch := filepath.Join(t.TempDir(), "builtin")
	if err := os.CopyFS(scratch, os.DirFS(source)); err != nil {
		t.Fatal(err)
	}
	if err := WriteBuiltins(scratch); err != nil {
		t.Fatal(err)
	}
	err := filepath.WalkDir(scratch, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		name := entry.Name()
		if name != "plugin.js" && name != "manifest.json" {
			return nil
		}
		rel, err := filepath.Rel(scratch, path)
		if err != nil {
			return err
		}
		want, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		got, err := os.ReadFile(filepath.Join(source, rel))
		if err != nil {
			return err
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s is stale; run go generate ./internal/jsplugin", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
