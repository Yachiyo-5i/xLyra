package config

import (
	"os"
	"path/filepath"
)

func ResolveWorkdir() string {
	if wd := os.Getenv("WORKDIR"); wd != "" {
		return wd
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fallbackHomeDir()
	}

	for dir := cwd; dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
	}

	return fallbackHomeDir()
}

func fallbackHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		cwd, _ := os.Getwd()
		return cwd
	}
	return filepath.Join(home, ".xlyra")
}

func ConfigDir(workdir string) string {
	return filepath.Join(workdir, "conf")
}

func ConfigFilePath(workdir string) string {
	return filepath.Join(ConfigDir(workdir), "config.json")
}
