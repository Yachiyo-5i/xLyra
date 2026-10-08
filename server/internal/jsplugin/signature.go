package jsplugin

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// SignatureFile is the signature.json payload inside a .xlp package.
type SignatureFile struct {
	Alg       string `json:"alg"`
	PublicKey string `json:"publicKey"`
	Sig       string `json:"sig"`
}

// VerifiedSignature is a package signature that passed cryptographic verification.
type VerifiedSignature struct {
	Fingerprint string
	PublicKey   []byte
}

func parseSignatureFile(raw []byte) (SignatureFile, error) {
	var file SignatureFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return SignatureFile{}, fmt.Errorf("signature.json is not valid JSON: %w", err)
	}
	file.Alg = strings.TrimSpace(file.Alg)
	file.PublicKey = strings.TrimSpace(file.PublicKey)
	file.Sig = strings.TrimSpace(file.Sig)
	if file.Alg == "" || file.PublicKey == "" || file.Sig == "" {
		return SignatureFile{}, fmt.Errorf("signature.json must include alg, publicKey, and sig")
	}
	return file, nil
}

func verifySignatureFile(digestHex string, raw []byte) (VerifiedSignature, error) {
	file, err := parseSignatureFile(raw)
	if err != nil {
		return VerifiedSignature{}, err
	}
	if file.Alg != "ed25519" {
		return VerifiedSignature{}, fmt.Errorf("unsupported signature algorithm %q", file.Alg)
	}
	pub, err := base64.StdEncoding.DecodeString(file.PublicKey)
	if err != nil {
		return VerifiedSignature{}, fmt.Errorf("publicKey is not base64: %w", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		return VerifiedSignature{}, fmt.Errorf("publicKey must be %d bytes", ed25519.PublicKeySize)
	}
	sig, err := base64.StdEncoding.DecodeString(file.Sig)
	if err != nil {
		return VerifiedSignature{}, fmt.Errorf("sig is not base64: %w", err)
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), packageSignMessage(digestHex), sig) {
		return VerifiedSignature{}, fmt.Errorf("package signature verification failed")
	}
	return VerifiedSignature{
		Fingerprint: PublicKeyFingerprint(pub),
		PublicKey:   pub,
	}, nil
}

// PublicKeyFingerprint returns the stable signer id stored on plugin versions.
func PublicKeyFingerprint(publicKey []byte) string {
	sum := sha256.Sum256(publicKey)
	return "SHA256:" + base64.StdEncoding.EncodeToString(sum[:])
}

// SignPackageDigest builds signature.json bytes for a canonical package digest.
func SignPackageDigest(digestHex string, privateKey ed25519.PrivateKey) ([]byte, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid ed25519 private key")
	}
	pub := privateKey.Public().(ed25519.PublicKey)
	sig := ed25519.Sign(privateKey, packageSignMessage(digestHex))
	payload := SignatureFile{
		Alg:       "ed25519",
		PublicKey: base64.StdEncoding.EncodeToString(pub),
		Sig:       base64.StdEncoding.EncodeToString(sig),
	}
	return json.Marshal(payload)
}

// AttachSignature writes signature.json into a copy of the plugin zip.
func AttachSignature(raw []byte, privateKey ed25519.PrivateKey) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("package is not a zip archive: %w", err)
	}
	files, err := readPackageFiles(reader)
	if err != nil {
		return nil, err
	}
	delete(files, signatureFileName)
	digest, err := CanonicalPackageDigest(files)
	if err != nil {
		return nil, err
	}
	sigJSON, err := SignPackageDigest(digest, privateKey)
	if err != nil {
		return nil, err
	}
	files[signatureFileName] = sigJSON
	return writePackageZip(files)
}

func writePackageZip(files map[string][]byte) ([]byte, error) {
	paths := make([]string, 0, len(files))
	for name := range files {
		paths = append(paths, name)
	}
	sort.Strings(paths)
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	for _, name := range paths {
		w, err := zw.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(files[name]); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
