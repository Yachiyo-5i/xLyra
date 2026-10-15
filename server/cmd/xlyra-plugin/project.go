package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/evanw/esbuild/pkg/api"

	"xlyra/server/internal/jsplugin"
)

// entryCandidates are tried in order under the project directory.
var entryCandidates = []string{"src/index.ts", "src/index.js"}

const fixtureRunTimeout = 5 * time.Second

type namedFixture struct {
	File    string
	Fixture jsplugin.Fixture
}

// builtProject is the package content produced from a project directory.
type builtProject struct {
	Files    map[string][]byte
	Fixtures []namedFixture
}

// buildProject bundles the project into the files a .xlp carries:
// manifest.json (with sha256 filled in), plugin.js and fixtures/*.json.
func buildProject(dir string) (*builtProject, error) {
	manifestRaw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("read manifest.json: %w", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		return nil, fmt.Errorf("manifest.json: %w", err)
	}
	source, err := bundle(dir)
	if err != nil {
		return nil, err
	}
	manifest["sha256"] = map[string]string{"plugin.js": jsplugin.HashSource(string(source))}
	manifestOut, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	built := &builtProject{Files: map[string][]byte{
		"manifest.json": append(manifestOut, '\n'),
		"plugin.js":     source,
	}}
	matches, err := filepath.Glob(filepath.Join(dir, "fixtures", "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	for _, match := range matches {
		raw, err := os.ReadFile(match)
		if err != nil {
			return nil, err
		}
		name := filepath.Base(match)
		var fixture jsplugin.Fixture
		if err := json.Unmarshal(raw, &fixture); err != nil {
			return nil, fmt.Errorf("fixtures/%s: %w", name, err)
		}
		built.Files["fixtures/"+name] = raw
		built.Fixtures = append(built.Fixtures, namedFixture{File: name, Fixture: fixture})
	}
	return built, nil
}

// bundle compiles the entry point to a single ES2023 module. Bare imports and
// dynamic import() are rejected: the sandbox has no module loader.
func bundle(dir string) ([]byte, error) {
	entry := ""
	for _, candidate := range entryCandidates {
		if _, err := os.Stat(filepath.Join(dir, candidate)); err == nil {
			entry = candidate
			break
		}
	}
	if entry == "" {
		return nil, fmt.Errorf("no entry point: create %s", strings.Join(entryCandidates, " or "))
	}
	result := api.Build(api.BuildOptions{
		AbsWorkingDir: mustAbs(dir),
		EntryPoints:   []string{entry},
		Bundle:        true,
		Write:         false,
		Format:        api.FormatESModule,
		Target:        api.ES2023,
		Platform:      api.PlatformNeutral,
		LogLevel:      api.LogLevelSilent,
		Plugins:       []api.Plugin{noExternalImports()},
	})
	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("build failed:\n%s", formatMessages(result.Errors))
	}
	if len(result.OutputFiles) != 1 {
		return nil, fmt.Errorf("build produced %d files, want 1", len(result.OutputFiles))
	}
	return result.OutputFiles[0].Contents, nil
}

func noExternalImports() api.Plugin {
	return api.Plugin{
		Name: "no-external-imports",
		Setup: func(build api.PluginBuild) {
			build.OnResolve(api.OnResolveOptions{Filter: ".*"}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if args.Kind == api.ResolveJSDynamicImport {
					return api.OnResolveResult{}, fmt.Errorf("dynamic import() is not allowed: %s", args.Path)
				}
				if args.Kind == api.ResolveEntryPoint {
					return api.OnResolveResult{}, nil
				}
				if !strings.HasPrefix(args.Path, ".") && !filepath.IsAbs(args.Path) {
					return api.OnResolveResult{}, fmt.Errorf("external import %q is not allowed; only relative files and `import type` can be used", args.Path)
				}
				return api.OnResolveResult{}, nil
			})
		},
	}
}

func formatMessages(messages []api.Message) string {
	lines := api.FormatMessages(messages, api.FormatMessagesOptions{Kind: api.ErrorMessage})
	return strings.TrimRight(strings.Join(lines, ""), "\n")
}

func mustAbs(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	return abs
}

// assemble zips the built files and loads them through the same path the
// server uses for an upload: ReadPackage, then CompilePackage.
func (b *builtProject) assemble() ([]byte, jsplugin.Package, *jsplugin.Plugin, error) {
	raw, err := jsplugin.BuildPackage(b.Files)
	if err != nil {
		return nil, jsplugin.Package{}, nil, err
	}
	pkg, plugin, err := loadPackage(raw)
	return raw, pkg, plugin, err
}

func loadPackage(raw []byte) (jsplugin.Package, *jsplugin.Plugin, error) {
	pkg, err := jsplugin.ReadPackage(raw, true)
	if err != nil {
		return jsplugin.Package{}, nil, err
	}
	plugin, err := jsplugin.CompilePackage(pkg)
	if err != nil {
		return pkg, nil, err
	}
	return pkg, plugin, nil
}

// runFixtures runs every fixture and prints one line each. It returns the failure count.
func runFixtures(plugin *jsplugin.Plugin, fixtures []namedFixture, out *bytes.Buffer) int {
	failed := 0
	for _, item := range fixtures {
		ctx, cancel := context.WithTimeout(context.Background(), fixtureRunTimeout)
		err := plugin.RunFixture(ctx, item.Fixture)
		cancel()
		label := item.File
		if item.Fixture.Name != "" {
			label += " (" + item.Fixture.Name + ")"
		}
		if err != nil {
			failed++
			fmt.Fprintf(out, "  FAIL %s\n       %v\n", label, err)
			continue
		}
		fmt.Fprintf(out, "  ok   %s\n", label)
	}
	return failed
}
