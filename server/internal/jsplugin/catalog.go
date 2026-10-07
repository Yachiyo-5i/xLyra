package jsplugin

import (
	"context"
	"fmt"
	"sync/atomic"
)

// Snapshot is one immutable registry generation.
type Snapshot struct {
	Gen      int64
	Registry *Registry
}

// Catalog holds the active plugin generation and coordinates hot swaps.
type Catalog struct {
	current atomic.Pointer[Snapshot]
	breaker *Breaker
}

var defaultCatalog = &Catalog{
	breaker: NewBreaker(nil),
}

// DefaultCatalog is the process-wide plugin catalog.
func DefaultCatalog() *Catalog {
	return defaultCatalog
}

// Current returns the active snapshot.
func (c *Catalog) Current() *Snapshot {
	if c == nil {
		return nil
	}
	return c.current.Load()
}

// Registry returns the active registry or nil.
func (c *Catalog) Registry() *Registry {
	snap := c.Current()
	if snap == nil {
		return nil
	}
	return snap.Registry
}

// Breaker returns the catalog circuit breaker.
func (c *Catalog) Breaker() *Breaker {
	if c == nil {
		return nil
	}
	return c.breaker
}

// Generation returns the active generation number.
func (c *Catalog) Generation() int64 {
	snap := c.Current()
	if snap == nil {
		return 0
	}
	return snap.Gen
}

// Replace swaps the active generation when next is non-nil.
func (c *Catalog) Replace(next *Snapshot) {
	if c == nil || next == nil || next.Registry == nil {
		return
	}
	c.current.Store(next)
}

// InitBuiltins loads embedded plugins into the catalog as generation 1.
func (c *Catalog) InitBuiltins() error {
	builtins, err := loadBuiltinFS()
	if err != nil {
		return err
	}
	c.Replace(&Snapshot{Gen: 1, Registry: builtins})
	return nil
}

// BuildGeneration merges builtins with uploaded enabled plugins.
func BuildGeneration(ctx context.Context, gen int64, enabled []EnabledPlugin, protocolSlugs map[string]string) (*Snapshot, error) {
	builtins, err := loadBuiltinFS()
	if err != nil {
		return nil, err
	}
	plugins := make([]*Plugin, 0, len(builtins.byID)+len(enabled))
	for _, plugin := range builtins.byID {
		plugins = append(plugins, plugin)
	}
	for _, item := range enabled {
		if item.Plugin == nil {
			continue
		}
		if _, ok := builtins.byID[item.Plugin.Manifest.ID]; ok {
			continue
		}
		if err := item.Plugin.SelfTest(ctx); err != nil {
			return nil, fmt.Errorf("%s@%s: selftest: %w", item.Plugin.Manifest.ID, item.Plugin.Manifest.Version, err)
		}
		plugins = append(plugins, item.Plugin)
	}
	merged := NewRegistry(plugins...)
	if len(protocolSlugs) > 0 {
		merged = merged.cloneWithSlugs(protocolSlugs)
	}
	return &Snapshot{Gen: gen, Registry: merged}, nil
}

// EnabledPlugin is one database-enabled plugin version.
type EnabledPlugin struct {
	Plugin *Plugin
}
