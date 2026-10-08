package main

import (
	"embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"xlyra/server/internal/jsplugin"
)

//go:embed templates
var templates embed.FS

type initData struct {
	ID   string
	Name string
	Slug string
}

var idSanitize = regexp.MustCompile(`[^a-z0-9.-]+`)

func runInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	kind := fs.String("kind", jsplugin.KindQuotaProbe, "plugin kind: quota_probe or protocol")
	id := fs.String("id", "", "plugin id (default: derived from the directory name)")
	name := fs.String("name", "", "display name (default: the id)")
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		fatal(fmt.Errorf("target directory is required"))
	}
	dir := fs.Arg(0)
	pluginID := *id
	if pluginID == "" {
		pluginID = strings.Trim(idSanitize.ReplaceAllString(strings.ToLower(filepath.Base(mustAbs(dir))), "-"), "-.")
	}
	if err := scaffold(dir, *kind, pluginID, *name); err != nil {
		fatal(err)
	}
	fmt.Fprintf(os.Stdout, "Created %s plugin %q in %s\nNext: cd %s && xlyra-plugin test\n", *kind, pluginID, dir, dir)
}

// scaffold writes a new plugin project into dir, which must be empty or missing.
func scaffold(dir, kind, pluginID, name string) error {
	if kind != jsplugin.KindQuotaProbe && kind != jsplugin.KindProtocol {
		return fmt.Errorf("unsupported kind %q", kind)
	}
	if err := jsplugin.ValidateUploadedID(pluginID); err != nil {
		return fmt.Errorf("%w (use --id)", err)
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s is not empty", dir)
	}
	data := initData{ID: pluginID, Name: name, Slug: strings.NewReplacer(".", "_", "-", "_").Replace(pluginID)}
	if data.Name == "" {
		data.Name = pluginID
	}
	files := map[string]string{
		"manifest.json":         kind + "/manifest.json.tmpl",
		"src/index.ts":          kind + "/src/index.ts.tmpl",
		"fixtures/example.json": kind + "/fixtures/example.json.tmpl",
		"tsconfig.json":         "common/tsconfig.json",
		"types/xlyra.d.ts":      "types/xlyra.d.ts",
		".gitignore":            "",
	}
	for target, source := range files {
		var content []byte
		switch {
		case target == ".gitignore":
			content = []byte("dist/\n")
		case strings.HasSuffix(source, ".tmpl"):
			rendered, err := renderTemplate(source, data)
			if err != nil {
				return err
			}
			content = rendered
		default:
			raw, err := templates.ReadFile("templates/" + source)
			if err != nil {
				return err
			}
			content = raw
		}
		path := filepath.Join(dir, filepath.FromSlash(target))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func renderTemplate(source string, data initData) ([]byte, error) {
	raw, err := templates.ReadFile("templates/" + source)
	if err != nil {
		return nil, err
	}
	tpl, err := template.New(source).Parse(string(raw))
	if err != nil {
		return nil, err
	}
	var out strings.Builder
	if err := tpl.Execute(&out, data); err != nil {
		return nil, err
	}
	return []byte(out.String()), nil
}
