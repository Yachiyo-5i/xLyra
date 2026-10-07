package jsplugin

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"sync"
)

//go:embed builtin
var builtinFS embed.FS

//go:generate go run ./genbuiltins

// Registry holds the built-in plugins loaded for this process.
type Registry struct {
	probes map[string]*Plugin
}

// NewRegistry maps each plugin by the probe type its manifest replaces.
func NewRegistry(plugins ...*Plugin) *Registry {
	registry := &Registry{probes: map[string]*Plugin{}}
	for _, plugin := range plugins {
		if plugin != nil && plugin.ProbeType() != "" {
			registry.probes[plugin.ProbeType()] = plugin
		}
	}
	return registry
}

// ByProbeType returns the built-in quota probe for a probe type such as "kimi".
func (r *Registry) ByProbeType(probeType string) (*Plugin, bool) {
	if r == nil {
		return nil, false
	}
	plugin, ok := r.probes[probeType]
	return plugin, ok
}

var (
	builtinOnce sync.Once
	builtinReg  *Registry
	builtinErr  error
)

// LoadBuiltins compiles the embedded quota probe plugins. A plugin that fails
// to compile is omitted; the joined error lists those failures.
func LoadBuiltins() (*Registry, error) {
	builtinOnce.Do(func() {
		builtinReg, builtinErr = loadBuiltinFS()
	})
	return builtinReg, builtinErr
}

func loadBuiltinFS() (*Registry, error) {
	registry := &Registry{probes: map[string]*Plugin{}}
	entries, err := fs.ReadDir(builtinFS, "builtin")
	if err != nil {
		return registry, err
	}
	var failures []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		plugin, err := loadBuiltinDir(path.Join("builtin", entry.Name()))
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		replaces := plugin.Manifest.QuotaProbe.Replaces
		if replaces == "" {
			failures = append(failures, plugin.Manifest.ID+": missing quotaProbe.replaces")
			continue
		}
		registry.probes[replaces] = plugin
	}
	if len(failures) > 0 {
		return registry, fmt.Errorf("builtin plugins: %s", strings.Join(failures, "; "))
	}
	return registry, nil
}

func loadBuiltinDir(dir string) (*Plugin, error) {
	manifestRaw, err := fs.ReadFile(builtinFS, path.Join(dir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	manifest, err := ParseManifest(manifestRaw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	source, err := fs.ReadFile(builtinFS, path.Join(dir, "plugin.js"))
	if err != nil {
		return nil, err
	}
	fixtures, err := loadFixtures(dir)
	if err != nil {
		return nil, err
	}
	return NewPlugin(manifest, string(source), fixtures, probePoolResident)
}

func loadFixtures(dir string) ([]Fixture, error) {
	fixtureDir := path.Join(dir, "fixtures")
	entries, err := fs.ReadDir(builtinFS, fixtureDir)
	if err != nil {
		return nil, err
	}
	var fixtures []Fixture
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := fs.ReadFile(builtinFS, path.Join(fixtureDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var fixture Fixture
		if err := json.Unmarshal(raw, &fixture); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		fixtures = append(fixtures, fixture)
	}
	return fixtures, nil
}
