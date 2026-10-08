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
	MaxPackageBytes   = 2 << 20
	MaxMergedSourceKiB = 512
)

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
	var manifestRaw []byte
	var source []byte
	var fixtures []Fixture
	var signatureRaw []byte
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name := path.Clean(strings.TrimPrefix(file.Name, "/"))
		switch {
		case name == "manifest.json":
			manifestRaw, err = readZipEntry(file, MaxPackageBytes)
			if err != nil {
				return Package{}, err
			}
		case name == "plugin.js":
			source, err = readZipEntry(file, MaxMergedSourceKiB<<10)
			if err != nil {
				return Package{}, err
			}
		case name == "signature" || name == "SIGNATURE":
			signatureRaw, err = readZipEntry(file, 4096)
			if err != nil {
				return Package{}, err
			}
		case strings.HasPrefix(name, "fixtures/") && strings.HasSuffix(name, ".json"):
			rawFixture, err := readZipEntry(file, MaxPackageBytes)
			if err != nil {
				return Package{}, err
			}
			var fixture Fixture
			if err := json.Unmarshal(rawFixture, &fixture); err != nil {
				return Package{}, fmt.Errorf("%s: %w", name, err)
			}
			fixtures = append(fixtures, fixture)
		}
	}
	if len(manifestRaw) == 0 {
		return Package{}, fmt.Errorf("manifest.json is required")
	}
	if len(source) == 0 {
		return Package{}, fmt.Errorf("plugin.js is required")
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
	sum := hashSource(string(source))
	if err := validateManifest(manifest, string(source)); err != nil {
		return Package{}, err
	}
	signer := ""
	if len(signatureRaw) > 0 {
		sig, err := parsePackageSignature(string(signatureRaw))
		if err != nil {
			return Package{}, fmt.Errorf("signature: %w", err)
		}
		if err := verifyPackageSignature(sum, sig); err != nil {
			return Package{}, err
		}
		signer = sig.Signer
	}
	return Package{
		Manifest: manifest,
		Source:   string(source),
		Fixtures: fixtures,
		SHA256:   sum,
		Signer:   signer,
	}, nil
}

func validateUploadedManifest(manifest Manifest) error {
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
