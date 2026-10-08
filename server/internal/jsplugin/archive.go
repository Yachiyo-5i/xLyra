package jsplugin

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
)

const (
	MaxPackageBytes    = 2 << 20
	MaxMergedSourceKiB = 512
)

var reservedPluginIDs = map[string]bool{
	"builtins": true,
}

// Package is a decoded .xlp upload before compile.
type Package struct {
	Manifest Manifest
	Source   string
	Fixtures []Fixture
	SHA256   string
	Signer   string
}

// ReadPackage validates and decodes a plugin zip archive.
func ReadPackage(raw []byte, requireFixtures bool) (Package, error) {
	if len(raw) == 0 {
		return Package{}, fmt.Errorf("package is empty")
	}
	if len(raw) > MaxPackageBytes {
		return Package{}, fmt.Errorf("package exceeds %d bytes", MaxPackageBytes)
	}
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return Package{}, fmt.Errorf("package is not a zip archive: %w", err)
	}
	files, err := readPackageFiles(reader)
	if err != nil {
		return Package{}, err
	}
	digest, err := CanonicalPackageDigest(files)
	if err != nil {
		return Package{}, err
	}
	manifestRaw := files["manifest.json"]
	source := files["plugin.js"]
	if len(manifestRaw) == 0 {
		return Package{}, fmt.Errorf("manifest.json is required")
	}
	if len(source) == 0 {
		return Package{}, fmt.Errorf("plugin.js is required")
	}
	var fixtures []Fixture
	for name, rawFixture := range files {
		if !strings.HasPrefix(name, "fixtures/") || !strings.HasSuffix(name, ".json") {
			continue
		}
		var fixture Fixture
		if err := json.Unmarshal(rawFixture, &fixture); err != nil {
			return Package{}, fmt.Errorf("%s: %w", name, err)
		}
		fixtures = append(fixtures, fixture)
	}
	if requireFixtures && len(fixtures) == 0 {
		return Package{}, fmt.Errorf("at least one fixtures/*.json is required")
	}
	manifest, err := ParseManifest(manifestRaw)
	if err != nil {
		return Package{}, err
	}
	if err := validateUploadedManifest(manifest); err != nil {
		return Package{}, err
	}
	if err := validateManifest(manifest, string(source)); err != nil {
		return Package{}, err
	}
	signer := ""
	if sigRaw := files[signatureFileName]; len(sigRaw) > 0 {
		verified, err := verifySignatureFile(digest, sigRaw)
		if err != nil {
			return Package{}, fmt.Errorf("signature: %w", err)
		}
		signer = verified.Fingerprint
	}
	return Package{
		Manifest: manifest,
		Source:   string(source),
		Fixtures: fixtures,
		SHA256:   digest,
		Signer:   signer,
	}, nil
}

func readPackageFiles(reader *zip.Reader) (map[string][]byte, error) {
	files := make(map[string][]byte)
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name := normalizeZipPath(file.Name)
		if name == "" || name == "." || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") {
			return nil, fmt.Errorf("invalid path %q", file.Name)
		}
		if _, exists := files[name]; exists {
			return nil, fmt.Errorf("duplicate path %q", name)
		}
		if len(files) >= maxPackageFiles {
			return nil, fmt.Errorf("package exceeds %d files", maxPackageFiles)
		}
		data, err := readZipEntry(file, MaxPackageBytes)
		if err != nil {
			return nil, err
		}
		files[name] = data
	}
	return files, nil
}

func normalizeZipPath(name string) string {
	name = path.Clean(strings.TrimPrefix(name, "/"))
	if name == "." {
		return ""
	}
	return name
}

func validateUploadedManifest(manifest Manifest) error {
	if reservedPluginIDs[manifest.ID] {
		return fmt.Errorf("plugin id %q is reserved", manifest.ID)
	}
	if strings.HasPrefix(manifest.ID, "xlyra.") {
		return fmt.Errorf("uploaded plugins cannot use the xlyra. prefix")
	}
	if manifest.Kind == KindQuotaProbe && manifest.QuotaProbe.Replaces != "" {
		return fmt.Errorf("uploaded quota_probe plugins cannot set quotaProbe.replaces")
	}
	return nil
}

func readZipEntry(file *zip.File, limit int) ([]byte, error) {
	rc, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("%s exceeds size limit", file.Name)
	}
	return data, nil
}

// CompilePackage builds a runtime plugin from a decoded package.
func CompilePackage(pkg Package) (*Plugin, error) {
	return NewPlugin(pkg.Manifest, pkg.Source, pkg.Fixtures, 0)
}

// Contract versions this build supports; the developer CLI prints them.
const (
	HookAPIVersion = hookAPIVersion
	HostAPIVersion = hostAPIVersion
)

// BuildPackage zips package files into a .xlp. It rejects archives that
// ReadPackage would refuse for size or file count, so pack fails early.
func BuildPackage(files map[string][]byte) ([]byte, error) {
	if _, err := CanonicalPackageDigest(files); err != nil {
		return nil, err
	}
	raw, err := writePackageZip(files)
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxPackageBytes {
		return nil, fmt.Errorf("package exceeds %d bytes", MaxPackageBytes)
	}
	return raw, nil
}

// ValidateUploadedID applies the id rules for uploaded plugins, so the CLI
// can reject a bad id at init time instead of at upload.
func ValidateUploadedID(id string) error {
	if !pluginIDPattern.MatchString(id) || len(id) < 3 || len(id) > 64 {
		return fmt.Errorf("invalid plugin id %q (3-64 chars of a-z, 0-9, '.', '-')", id)
	}
	return validateUploadedManifest(Manifest{ID: id})
}
