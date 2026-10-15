package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

func runBuild(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	out := fs.String("out", "dist", "output directory")
	_ = fs.Parse(args)
	dir := "."
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}
	built, err := buildProject(dir)
	if err != nil {
		fatal(err)
	}
	if _, pkg, _, err := built.assemble(); err != nil {
		fatal(err)
	} else {
		target := *out
		if !filepath.IsAbs(target) {
			target = filepath.Join(dir, target)
		}
		if err := writeTree(target, built.Files); err != nil {
			fatal(err)
		}
		fmt.Fprintf(os.Stdout, "built %s@%s -> %s\n", pkg.Manifest.ID, pkg.Manifest.Version, target)
	}
}

func writeTree(root string, files map[string][]byte) error {
	// Replace only the files a build owns; dist/ may also hold .xlp packages.
	for _, stale := range []string{"manifest.json", "plugin.js", "fixtures"} {
		if err := os.RemoveAll(filepath.Join(root, stale)); err != nil {
			return err
		}
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, files[name], 0o644); err != nil {
			return err
		}
	}
	return nil
}
