package version

import "testing"

func TestCurrentDefaultsBlankBuildFields(t *testing.T) {
	oldVersion, oldCommit, oldBuildTime := Version, Commit, BuildTime
	t.Cleanup(func() {
		Version, Commit, BuildTime = oldVersion, oldCommit, oldBuildTime
	})
	Version = " "
	Commit = " "
	BuildTime = " "

	info := Current()

	if info.Version != "dev" {
		t.Fatalf("Version = %q, want dev", info.Version)
	}
	if info.Commit != "unknown" {
		t.Fatalf("Commit = %q, want unknown", info.Commit)
	}
	if info.BuildTime != "" {
		t.Fatalf("BuildTime = %q, want empty", info.BuildTime)
	}
}

func TestCurrentTrimsBuildFields(t *testing.T) {
	oldVersion, oldCommit, oldBuildTime := Version, Commit, BuildTime
	t.Cleanup(func() {
		Version, Commit, BuildTime = oldVersion, oldCommit, oldBuildTime
	})
	Version = " 1.2.3 "
	Commit = " abc123 "
	BuildTime = " 2026-06-22T00:00:00Z "

	info := Current()

	if info.Version != "1.2.3" || info.Commit != "abc123" || info.BuildTime != "2026-06-22T00:00:00Z" {
		t.Fatalf("unexpected version info: %#v", info)
	}
}
