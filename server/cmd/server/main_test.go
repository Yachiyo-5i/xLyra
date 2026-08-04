package main

import (
	"path/filepath"
	"testing"
)

func TestResolveLogDirUsesWorkdirForDefaultLogDirs(t *testing.T) {
	t.Parallel()

	workdir := filepath.Join(string(filepath.Separator), "var", "lib", "xlyra")
	for _, configured := range []string{"", "logs", "data/logs"} {
		got := resolveLogDir(workdir, configured)
		want := filepath.Join(workdir, "logs")
		if got != want {
			t.Fatalf("resolveLogDir(%q) = %q, want %q", configured, got, want)
		}
	}
}

func TestResolveLogDirKeepsAbsoluteAndResolvesRelative(t *testing.T) {
	t.Parallel()

	workdir := filepath.Join(string(filepath.Separator), "var", "lib", "xlyra")
	absolute := filepath.Join(string(filepath.Separator), "tmp", "xlyra-logs")
	if got := resolveLogDir(workdir, absolute); got != absolute {
		t.Fatalf("absolute log dir = %q, want %q", got, absolute)
	}

	if got := resolveLogDir(workdir, "runtime/logs"); got != filepath.Join(workdir, "runtime/logs") {
		t.Fatalf("relative log dir = %q", got)
	}
}
