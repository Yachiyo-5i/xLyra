package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestResolveWorkdir_EnvVar(t *testing.T) {
	t.Setenv("WORKDIR", "/custom/workdir")
	got := ResolveWorkdir()
	if got != "/custom/workdir" {
		t.Fatalf("expected /custom/workdir, got %s", got)
	}
}

func TestResolveWorkdir_GitRoot(t *testing.T) {
	got := ResolveWorkdir()
	if got == "" {
		t.Fatal("expected non-empty workdir")
	}

	fi, err := os.Stat(filepath.Join(got, ".git"))
	if err != nil {
		t.Fatalf("resolved workdir %s should contain .git: %v", got, err)
	}
	if !fi.IsDir() && !fi.Mode().IsRegular() {
		t.Fatal(".git should be a directory or worktree file")
	}
}

func TestResolveWorkdir_FallsBackToHomeWhenNoGitRoot(t *testing.T) {
	t.Setenv("WORKDIR", "")

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})

	nestedDir := filepath.Join(t.TempDir(), "nested", "project")
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatalf("mkdir nested dir: %v", err)
	}
	if err := os.Chdir(nestedDir); err != nil {
		t.Fatalf("chdir nested dir: %v", err)
	}

	got := ResolveWorkdir()
	expected := filepath.Join(homeDir, ".xlyra")
	if got != expected {
		t.Fatalf("expected fallback workdir %s, got %s", expected, got)
	}
}

func TestConfigDir(t *testing.T) {
	got := ConfigDir("/tmp/xlyra")
	expected := filepath.Join("/tmp/xlyra", "conf")
	if got != expected {
		t.Fatalf("expected %s, got %s", expected, got)
	}
}

func TestConfigFilePath(t *testing.T) {
	got := ConfigFilePath("/tmp/xlyra")
	expected := filepath.Join("/tmp/xlyra", "conf", "config.json")
	if got != expected {
		t.Fatalf("expected %s, got %s", expected, got)
	}
}

func TestLoadConfigFile_CreatesDefault(t *testing.T) {
	dir := t.TempDir()

	cf, err := LoadConfigFile(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify file was created on disk
	path := ConfigFilePath(dir)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config file should exist: %v", err)
	}

	// Verify data has defaults
	data := cf.Data()
	for _, cat := range []string{"global", "network", "notification"} {
		if _, ok := data[cat]; !ok {
			t.Fatalf("missing category %q in config", cat)
		}
	}

	// Verify file content is valid JSON
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("file is not valid JSON: %v", err)
	}
}

func TestLoadConfigFile_LoadsExisting(t *testing.T) {
	dir := t.TempDir()
	confDir := ConfigDir(dir)
	path := ConfigFilePath(dir)

	if err := os.MkdirAll(confDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	custom := map[string]any{
		"global": map[string]any{
			"app_name": "custom-app",
		},
	}
	raw, _ := json.MarshalIndent(custom, "", "  ")
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	cf, err := LoadConfigFile(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	val, ok := cf.Get("global.app_name")
	if !ok {
		t.Fatal("expected global.app_name to exist")
	}
	if val != "custom-app" {
		t.Fatalf("expected 'custom-app', got %v", val)
	}
}

func TestGetSet(t *testing.T) {
	dir := t.TempDir()
	cf, err := LoadConfigFile(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Set nested path
	if err := cf.Set("global.app_name", "updated-app"); err != nil {
		t.Fatalf("set: %v", err)
	}

	val, ok := cf.Get("global.app_name")
	if !ok {
		t.Fatal("key should exist")
	}
	if val != "updated-app" {
		t.Fatalf("expected 'updated-app', got %v", val)
	}

	// Set new path
	if err := cf.Set("global.new_key", "new_value"); err != nil {
		t.Fatalf("set new key: %v", err)
	}
	val, ok = cf.Get("global.new_key")
	if !ok {
		t.Fatal("new key should exist")
	}
	if val != "new_value" {
		t.Fatalf("expected 'new_value', got %v", val)
	}

	// Get non-existent key
	_, ok = cf.Get("nonexistent.key")
	if ok {
		t.Fatal("expected key to not exist")
	}

	// Verify persistence to disk
	cf2, err := LoadConfigFile(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	val, ok = cf2.Get("global.app_name")
	if !ok || val != "updated-app" {
		t.Fatalf("persisted value mismatch: %v", val)
	}
}

func TestMerge(t *testing.T) {
	dir := t.TempDir()
	cf, err := LoadConfigFile(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updates := map[string]any{
		"global": map[string]any{
			"app_name":  "merged-app",
			"log_level": "debug",
		},
	}
	if err := cf.Merge(updates); err != nil {
		t.Fatalf("merge: %v", err)
	}

	// Merge replaces top-level keys entirely
	data := cf.Data()
	global, ok := data["global"].(map[string]any)
	if !ok {
		t.Fatal("global should be a map")
	}
	if global["app_name"] != "merged-app" {
		t.Fatalf("expected 'merged-app', got %v", global["app_name"])
	}
	if global["log_level"] != "debug" {
		t.Fatalf("expected 'debug', got %v", global["log_level"])
	}
	// Merged global only has the two keys since it replaces top-level
	if len(global) != 2 {
		t.Fatalf("merge should replace top-level keys entirely, got %d keys", len(global))
	}
}

func TestReload(t *testing.T) {
	dir := t.TempDir()
	cf, err := LoadConfigFile(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Modify file directly on disk
	path := ConfigFilePath(dir)
	custom := map[string]any{
		"global": map[string]any{
			"app_name": "reloaded-app",
		},
	}
	raw, _ := json.MarshalIndent(custom, "", "  ")
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Reload
	if err := cf.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}

	val, ok := cf.Get("global.app_name")
	if !ok {
		t.Fatal("key should exist")
	}
	if val != "reloaded-app" {
		t.Fatalf("expected 'reloaded-app', got %v", val)
	}
}

func TestReload_FileRemoved(t *testing.T) {
	dir := t.TempDir()
	cf, err := LoadConfigFile(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	path := ConfigFilePath(dir)
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}

	err = cf.Reload()
	if err == nil {
		t.Fatal("expected error when config file is removed")
	}
}

func TestOnChange(t *testing.T) {
	dir := t.TempDir()
	cf, err := LoadConfigFile(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var mu sync.Mutex
	var received map[string]any
	cf.OnChange(func(data map[string]any) {
		mu.Lock()
		received = data
		mu.Unlock()
	})

	if err := cf.Set("global.app_name", "callback-test"); err != nil {
		t.Fatalf("set: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if received == nil {
		t.Fatal("expected callback to be called")
	}
	if received["global"].(map[string]any)["app_name"] != "callback-test" {
		t.Fatal("callback received wrong data")
	}
}

func TestConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	cf, err := LoadConfigFile(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var wg sync.WaitGroup
	n := 50

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cf.Set("global.app_name", "concurrent")
		}()
	}

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = cf.Get("global.app_name")
		}()
	}

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cf.Data()
		}()
	}

	wg.Wait()

	// Final value should be consistent
	val, ok := cf.Get("global.app_name")
	if !ok || val != "concurrent" {
		t.Fatalf("expected 'concurrent', got %v", val)
	}
}

func TestData_ReturnsDeepCopy(t *testing.T) {
	dir := t.TempDir()
	cf, err := LoadConfigFile(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := cf.Set("global.app_name", "original"); err != nil {
		t.Fatalf("set: %v", err)
	}

	data := cf.Data()
	data["global"].(map[string]any)["app_name"] = "mutated"

	// Original should be unchanged
	val, ok := cf.Get("global.app_name")
	if !ok {
		t.Fatal("key should exist")
	}
	if val != "original" {
		t.Fatalf("Data() should return a deep copy, mutation leaked: got %v", val)
	}
}

func TestPathReplaceAndDeepCopySliceBoundaries(t *testing.T) {
	dir := t.TempDir()
	cf, err := LoadConfigFile(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cf.Path() != ConfigFilePath(dir) {
		t.Fatalf("Path() = %q, want %q", cf.Path(), ConfigFilePath(dir))
	}

	replacement := map[string]any{
		"global": map[string]any{
			"app_name": "replaced",
			"items": []any{
				map[string]any{"name": "nested"},
				[]any{"inner"},
			},
			"labels": []string{"alpha", "beta"},
		},
	}
	if err := cf.Replace(replacement); err != nil {
		t.Fatalf("replace: %v", err)
	}
	replacement["global"].(map[string]any)["app_name"] = "mutated"
	replacement["global"].(map[string]any)["items"].([]any)[0].(map[string]any)["name"] = "mutated"
	replacement["global"].(map[string]any)["labels"].([]string)[0] = "mutated"

	data := cf.Data()
	global := data["global"].(map[string]any)
	if global["app_name"] != "replaced" {
		t.Fatalf("Replace should deep-copy map values, got %#v", global)
	}
	items := global["items"].([]any)
	if items[0].(map[string]any)["name"] != "nested" {
		t.Fatalf("nested slice map mutation leaked into config: %#v", items)
	}
	labels := global["labels"].([]string)
	if labels[0] != "alpha" {
		t.Fatalf("[]string mutation leaked into config: %#v", labels)
	}

	source := []any{map[string]any{"k": "v"}, []any{map[string]any{"nested": "ok"}}}
	copied := deepCopySlice(source)
	copied[0].(map[string]any)["k"] = "changed"
	copied[1].([]any)[0].(map[string]any)["nested"] = "changed"
	if source[0].(map[string]any)["k"] != "v" || source[1].([]any)[0].(map[string]any)["nested"] != "ok" {
		t.Fatalf("sanity source changed unexpectedly: %#v", source)
	}
	if deepCopySlice(nil) != nil {
		t.Fatal("deepCopySlice(nil) should return nil")
	}
}

func TestStartWatcherReloadsAndStopWatcherIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	cf, err := LoadConfigFile(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cf.StartWatcher(ctx, time.Millisecond)
	cf.StartWatcher(ctx, time.Millisecond)

	path := ConfigFilePath(dir)
	updated := map[string]any{
		"global": map[string]any{
			"app_name": "watched",
		},
	}
	raw, _ := json.MarshalIndent(updated, "", "  ")
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatalf("write watched file: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if val, ok := cf.Get("global.app_name"); ok && val == "watched" {
			cf.StopWatcher()
			cf.StopWatcher()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	cf.StopWatcher()
	t.Fatal("watcher did not reload updated config before deadline")
}
