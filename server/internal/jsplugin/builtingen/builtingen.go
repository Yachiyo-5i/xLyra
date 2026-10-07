// Package builtingen writes the self-contained plugin.js of each built-in
// plugin. Only go generate and tests use it; the server binary does not.
package builtingen

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// WriteBuiltins concatenates root/_shared/*.js in front of each plugin's
// src/plugin.js, writes plugin.js, and refreshes the manifest sha256.
func WriteBuiltins(root string) error {
	shared, err := readShared(filepath.Join(root, "_shared"))
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		if err := writePlugin(filepath.Join(root, entry.Name()), shared); err != nil {
			return err
		}
	}
	return nil
}

func writePlugin(dir, shared string) error {
	source, err := os.ReadFile(filepath.Join(dir, "src", "plugin.js"))
	if err != nil {
		return err
	}
	combined := shared + string(source)
	if err := os.WriteFile(filepath.Join(dir, "plugin.js"), []byte(combined), 0o644); err != nil {
		return err
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var manifest map[string]any
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return err
	}
	sums, _ := manifest["sha256"].(map[string]any)
	if sums == nil {
		sums = map[string]any{}
	}
	sum := sha256.Sum256([]byte(combined))
	sums["plugin.js"] = hex.EncodeToString(sum[:])
	manifest["sha256"] = sums
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(manifest); err != nil {
		return err
	}
	return os.WriteFile(manifestPath, encoded.Bytes(), 0o644)
}

func readShared(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".js") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return "", err
		}
		b.Write(body)
		if !bytes.HasSuffix(body, []byte("\n")) {
			b.WriteByte('\n')
		}
	}
	return b.String(), nil
}
