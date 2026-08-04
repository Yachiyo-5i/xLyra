package main

import (
	"path/filepath"
	"testing"
)

func TestResolveLogDirHandlesDotAndNestedRelativePaths(t *testing.T) {
	t.Parallel()

	workdir := filepath.Join(string(filepath.Separator), "var", "lib", "xlyra")
	for _, tc := range []struct {
		name       string
		configured string
		want       string
	}{
		{name: "dot", configured: ".", want: workdir},
		{name: "nested under logs", configured: "logs/current", want: filepath.Join(workdir, "logs", "current")},
		{name: "nested under data logs", configured: "data/logs/archive", want: filepath.Join(workdir, "data", "logs", "archive")},
		{name: "clean relative path", configured: "./runtime/../logs", want: filepath.Join(workdir, "logs")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := resolveLogDir(workdir, tc.configured); got != tc.want {
				t.Fatalf("resolveLogDir(%q) = %q, want %q", tc.configured, got, tc.want)
			}
		})
	}
}
