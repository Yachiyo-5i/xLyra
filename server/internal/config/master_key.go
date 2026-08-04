package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const masterKeyFileMode os.FileMode = 0600

func MasterKeyPath(workdir string) string {
	return filepath.Join(ConfigDir(workdir), "master.key")
}

func LoadMasterKey(workdir string) (string, error) {
	path := MasterKeyPath(workdir)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", fmt.Errorf("ensure master key dir: %w", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("read master key: %w", err)
		}
		key, err := newMasterKey()
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(path, []byte(key+"\n"), masterKeyFileMode); err != nil {
			return "", fmt.Errorf("write master key: %w", err)
		}
		if err := os.Chmod(path, masterKeyFileMode); err != nil {
			return "", fmt.Errorf("set master key permissions: %w", err)
		}
		return key, nil
	}

	key := strings.TrimSpace(string(raw))
	if key == "" {
		return "", fmt.Errorf("master key file is empty: %s", path)
	}
	if err := os.Chmod(path, masterKeyFileMode); err != nil {
		return "", fmt.Errorf("set master key permissions: %w", err)
	}
	return key, nil
}

func newMasterKey() (string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate master key: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(random), nil
}
