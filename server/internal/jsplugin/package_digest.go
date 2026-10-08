package jsplugin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

const (
	signatureFileName   = "signature.json"
	maxPackageFiles     = 64
	packageDigestPrefix = "xlyra-plugin-package-v1\n"
)

// CanonicalPackageDigest hashes every file except signature.json (paths sorted).
func CanonicalPackageDigest(files map[string][]byte) (string, error) {
	if len(files) == 0 {
		return "", fmt.Errorf("package has no files")
	}
	if len(files) > maxPackageFiles {
		return "", fmt.Errorf("package exceeds %d files", maxPackageFiles)
	}
	paths := make([]string, 0, len(files))
	for name := range files {
		if name == signatureFileName {
			continue
		}
		paths = append(paths, name)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, path := range paths {
		fileHash := sha256.Sum256(files[path])
		if _, err := fmt.Fprintf(h, "%s %s\n", path, hex.EncodeToString(fileHash[:])); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func packageSignMessage(digestHex string) []byte {
	return []byte(packageDigestPrefix + strings.TrimSpace(digestHex))
}
