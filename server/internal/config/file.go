package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type ConfigFile struct {
	mu       sync.RWMutex
	path     string
	data     map[string]any
	watchers []func(map[string]any)
	stopCh   chan struct{}
}

type PreparedConfigReplace struct {
	config *ConfigFile
	data   map[string]any
	path   string
}

func LoadConfigFile(workdir string) (*ConfigFile, error) {
	path := ConfigFilePath(workdir)
	cf := &ConfigFile{path: path}

	if err := cf.ensureDir(); err != nil {
		return nil, fmt.Errorf("ensure config dir: %w", err)
	}

	data, err := cf.readFile()
	if err != nil {
		if os.IsNotExist(err) {
			cf.data = deepCopyMap(Defaults())
			if err := cf.save(); err != nil {
				return nil, fmt.Errorf("write defaults: %w", err)
			}
			return cf, nil
		}
		return nil, fmt.Errorf("read config file: %w", err)
	}

	cf.data = data
	return cf, nil
}

func (c *ConfigFile) Path() string {
	return c.path
}

func (c *ConfigFile) Data() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return deepCopyMap(c.data)
}

func (c *ConfigFile) Get(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return getByPath(c.data, key)
}

func (c *ConfigFile) Set(key string, value any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	setByPath(c.data, key, value)
	if err := c.save(); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	c.notify()
	return nil
}

func (c *ConfigFile) Merge(updates map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for k, v := range updates {
		c.data[k] = v
	}

	if err := c.save(); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	c.notify()
	return nil
}

func (c *ConfigFile) Replace(data map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data = deepCopyMap(data)
	if err := c.save(); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	c.notify()
	return nil
}

func (c *ConfigFile) PrepareReplace(data map[string]any) (*PreparedConfigReplace, error) {
	preparedData := deepCopyMap(data)
	encoded, err := json.MarshalIndent(preparedData, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(c.path), ".config-restore-*")
	if err != nil {
		return nil, fmt.Errorf("create staged config: %w", err)
	}
	stagedPath := file.Name()
	success := false
	defer func() {
		if !success {
			_ = file.Close()
			_ = os.Remove(stagedPath)
		}
	}()
	if err := file.Chmod(0644); err != nil {
		return nil, fmt.Errorf("set staged config permissions: %w", err)
	}
	if _, err := file.Write(encoded); err != nil {
		return nil, fmt.Errorf("write staged config: %w", err)
	}
	if err := file.Sync(); err != nil {
		return nil, fmt.Errorf("sync staged config: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close staged config: %w", err)
	}
	success = true
	return &PreparedConfigReplace{config: c, data: preparedData, path: stagedPath}, nil
}

func (p *PreparedConfigReplace) Commit() error {
	p.config.mu.Lock()
	defer p.config.mu.Unlock()
	if p.path == "" {
		return fmt.Errorf("staged config is unavailable")
	}
	if err := os.Rename(p.path, p.config.path); err != nil {
		return fmt.Errorf("replace config file: %w", err)
	}
	p.path = ""
	p.config.data = deepCopyMap(p.data)
	p.config.notify()
	return nil
}

func (p *PreparedConfigReplace) Discard() {
	if p == nil || p.path == "" {
		return
	}
	_ = os.Remove(p.path)
	p.path = ""
}

func (c *ConfigFile) Reload() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := c.readFile()
	if err != nil {
		return fmt.Errorf("reload config file: %w", err)
	}

	c.data = data
	c.notify()
	return nil
}

func (c *ConfigFile) OnChange(fn func(data map[string]any)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.watchers = append(c.watchers, fn)
}

func (c *ConfigFile) StartWatcher(ctx context.Context, interval time.Duration) {
	c.mu.Lock()
	if c.stopCh != nil {
		c.mu.Unlock()
		return
	}
	c.stopCh = make(chan struct{})
	stopCh := c.stopCh
	c.mu.Unlock()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-stopCh:
				return
			case <-ticker.C:
				_ = c.Reload()
			}
		}
	}()
}

func (c *ConfigFile) StopWatcher() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopCh != nil {
		close(c.stopCh)
		c.stopCh = nil
	}
}

func (c *ConfigFile) ensureDir() error {
	dir := filepath.Dir(c.path)
	return os.MkdirAll(dir, 0755)
}

func (c *ConfigFile) save() error {
	encoded, err := json.MarshalIndent(c.data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	tmpPath := c.path + ".tmp"
	if err := os.WriteFile(tmpPath, encoded, 0644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}

	return os.Rename(tmpPath, c.path)
}

func (c *ConfigFile) readFile() (map[string]any, error) {
	raw, err := os.ReadFile(c.path)
	if err != nil {
		return nil, err
	}

	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	if data == nil {
		data = map[string]any{}
	}

	return data, nil
}

func (c *ConfigFile) notify() {
	data := deepCopyMap(c.data)
	for _, fn := range c.watchers {
		fn(data)
	}
}

func getByPath(data map[string]any, key string) (any, bool) {
	parts := strings.Split(key, ".")
	if len(parts) == 0 {
		return nil, false
	}

	current := data
	for _, part := range parts[:len(parts)-1] {
		val, ok := current[part]
		if !ok {
			return nil, false
		}

		next, ok := val.(map[string]any)
		if !ok {
			return nil, false
		}
		current = next
	}

	val, ok := current[parts[len(parts)-1]]
	return val, ok
}

func setByPath(data map[string]any, key string, value any) {
	parts := strings.Split(key, ".")
	if len(parts) == 0 {
		return
	}

	current := data
	for _, part := range parts[:len(parts)-1] {
		next, ok := current[part]
		if !ok {
			next = map[string]any{}
			current[part] = next
		}
		m, ok := next.(map[string]any)
		if !ok {
			m = map[string]any{}
			current[part] = m
		}
		current = m
	}

	current[parts[len(parts)-1]] = value
}

func deepCopyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}

	dst := make(map[string]any, len(src))
	for k, v := range src {
		switch val := v.(type) {
		case map[string]any:
			dst[k] = deepCopyMap(val)
		case []any:
			dst[k] = deepCopySlice(val)
		case []string:
			dst[k] = append([]string(nil), val...)
		default:
			dst[k] = v
		}
	}
	return dst
}

func deepCopySlice(src []any) []any {
	if src == nil {
		return nil
	}

	dst := make([]any, len(src))
	for i, v := range src {
		switch val := v.(type) {
		case map[string]any:
			dst[i] = deepCopyMap(val)
		case []any:
			dst[i] = deepCopySlice(val)
		default:
			dst[i] = v
		}
	}
	return dst
}
