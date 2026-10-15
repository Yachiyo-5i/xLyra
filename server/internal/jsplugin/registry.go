package jsplugin

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
)

//go:embed builtin
var builtinFS embed.FS

//go:generate go run ./genbuiltins

// Registry holds compiled plugins for one generation.
type Registry struct {
	probes         map[string]*Plugin
	protocols      map[string]*Plugin
	byID           map[string]*Plugin
	protocolSlugs  map[string]*Plugin
}

// NewRegistry maps plugins by probe type, protocol name, manifest id, and slug.
func NewRegistry(plugins ...*Plugin) *Registry {
	registry := &Registry{
		probes:        map[string]*Plugin{},
		protocols:     map[string]*Plugin{},
		byID:          map[string]*Plugin{},
		protocolSlugs: map[string]*Plugin{},
	}
	for _, plugin := range plugins {
		registry.add(plugin, "")
	}
	return registry
}

func (r *Registry) add(plugin *Plugin, protocolSlug string) {
	if r == nil || plugin == nil {
		return
	}
	r.byID[plugin.Manifest.ID] = plugin
	switch plugin.Manifest.Kind {
	case KindQuotaProbe:
		if replaces := plugin.ProbeType(); replaces != "" {
			r.probes[replaces] = plugin
		}
	case KindProtocol:
		if name := plugin.ProtocolName(); name != "" {
			r.protocols[name] = plugin
		}
		if slug := strings.TrimSpace(protocolSlug); slug != "" {
			r.protocolSlugs[slug] = plugin
		}
	}
}

// ByProbeType returns the built-in quota probe for a probe type such as "kimi".
func (r *Registry) ByProbeType(probeType string) (*Plugin, bool) {
	if r == nil {
		return nil, false
	}
	plugin, ok := r.probes[probeType]
	return plugin, ok
}

// ByProtocolName returns a protocol plugin such as "typesafe_systemone".
func (r *Registry) ByProtocolName(name string) (*Plugin, bool) {
	if r == nil {
		return nil, false
	}
	plugin, ok := r.protocols[name]
	return plugin, ok
}

// Plugins returns every plugin in the registry sorted by manifest id.
func (r *Registry) Plugins() []*Plugin {
	if r == nil || len(r.byID) == 0 {
		return nil
	}
	out := make([]*Plugin, 0, len(r.byID))
	for _, plugin := range r.byID {
		out = append(out, plugin)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Manifest.ID < out[j].Manifest.ID
	})
	return out
}

// ByKind returns the enabled plugins of one kind, ordered by id so that the
// outcome does not depend on map order.
func (r *Registry) ByKind(kind string) []*Plugin {
	if r == nil {
		return nil
	}
	var out []*Plugin
	for _, plugin := range r.byID {
		if plugin.Manifest.Kind == kind {
			out = append(out, plugin)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Manifest.ID < out[j].Manifest.ID })
	return out
}

// ByPluginID returns a plugin by manifest id.
func (r *Registry) ByPluginID(id string) (*Plugin, bool) {
	if r == nil {
		return nil, false
	}
	plugin, ok := r.byID[id]
	return plugin, ok
}

// ProtocolBySlug returns an enabled third-party protocol binding.
func (r *Registry) ProtocolBySlug(slug string) (*Plugin, bool) {
	if r == nil {
		return nil, false
	}
	plugin, ok := r.protocolSlugs[slug]
	return plugin, ok
}

// ProtocolSlugs returns slug to plugin bindings for enabled third-party protocols.
func (r *Registry) ProtocolSlugs() map[string]*Plugin {
	if r == nil || len(r.protocolSlugs) == 0 {
		return nil
	}
	out := make(map[string]*Plugin, len(r.protocolSlugs))
	for slug, plugin := range r.protocolSlugs {
		out[slug] = plugin
	}
	return out
}

// Generation is the registry generation counter exposed to logs.
func (r *Registry) cloneWithSlugs(slugs map[string]string) *Registry {
	if r == nil {
		return NewRegistry()
	}
	out := &Registry{
		probes:        map[string]*Plugin{},
		protocols:     map[string]*Plugin{},
		byID:          map[string]*Plugin{},
		protocolSlugs: map[string]*Plugin{},
	}
	for key, plugin := range r.probes {
		out.probes[key] = plugin
	}
	for key, plugin := range r.protocols {
		out.protocols[key] = plugin
	}
	for key, plugin := range r.byID {
		out.byID[key] = plugin
	}
	for slug, pluginID := range slugs {
		if plugin, ok := r.byID[pluginID]; ok {
			out.protocolSlugs[slug] = plugin
		}
	}
	return out
}

var (
	builtinOnce sync.Once
	builtinReg  *Registry
	builtinErr  error
)

// LoadBuiltins compiles the embedded plugins. A plugin that fails to compile is
// omitted; the joined error lists those failures.
func LoadBuiltins() (*Registry, error) {
	builtinOnce.Do(func() {
		builtinReg, builtinErr = loadBuiltinFS()
	})
	return builtinReg, builtinErr
}

func loadBuiltinFS() (*Registry, error) {
	entries, err := fs.ReadDir(builtinFS, "builtin")
	if err != nil {
		return NewRegistry(), err
	}
	var failures []string
	var plugins []*Plugin
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		plugin, err := loadBuiltinDir(path.Join("builtin", entry.Name()))
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		switch plugin.Manifest.Kind {
		case KindQuotaProbe:
			if plugin.Manifest.QuotaProbe.Replaces == "" {
				failures = append(failures, plugin.Manifest.ID+": missing quotaProbe.replaces")
				continue
			}
		case KindProtocol:
			if plugin.Manifest.Protocol.Name == "" {
				failures = append(failures, plugin.Manifest.ID+": missing protocol.name")
				continue
			}
		default:
			failures = append(failures, plugin.Manifest.ID+": unsupported kind "+plugin.Manifest.Kind)
			continue
		}
		plugins = append(plugins, plugin)
	}
	if len(failures) > 0 {
		return NewRegistry(), fmt.Errorf("builtin plugins: %s", strings.Join(failures, "; "))
	}
	return NewRegistry(plugins...), nil
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
	return NewPlugin(manifest, string(source), fixtures, 0)
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
