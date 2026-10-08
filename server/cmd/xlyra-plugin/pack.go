package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"xlyra/server/internal/jsplugin"
)

func runPack(args []string) {
	fs := flag.NewFlagSet("pack", flag.ExitOnError)
	keyPath := fs.String("sign", "", "ed25519 private key PEM; signs the package")
	outPath := fs.String("out", "", "output .xlp path (default: dist/<id>-<version>.xlp)")
	_ = fs.Parse(args)
	dir := "."
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}
	built, err := buildProject(dir)
	if err != nil {
		fatal(err)
	}
	raw, pkg, _, err := built.assemble()
	if err != nil {
		fatal(err)
	}
	if *keyPath != "" {
		priv, err := loadPrivateKey(*keyPath)
		if err != nil {
			fatal(err)
		}
		if raw, err = jsplugin.AttachSignature(raw, priv); err != nil {
			fatal(err)
		}
	}
	// Refuse to write a package the server would reject.
	if err := verifyPackage(raw); err != nil {
		fatal(err)
	}
	target := *outPath
	if target == "" {
		target = filepath.Join(dir, "dist", fmt.Sprintf("%s-%s.xlp", pkg.Manifest.ID, pkg.Manifest.Version))
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(target, raw, 0o644); err != nil {
		fatal(err)
	}
	fmt.Fprintf(os.Stderr, "Wrote %s\n", target)
}
