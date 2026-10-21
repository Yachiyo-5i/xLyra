package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"xlyra/server/internal/jsplugin"
)

func runVerify(args []string) {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		fatal(fmt.Errorf("package path is required"))
	}
	raw, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		fatal(err)
	}
	if err := verifyPackage(raw); err != nil {
		fatal(err)
	}
}

// verifyPackage applies the checks the server runs on upload: static
// validation, compile, and fixtures. On success it prints a summary.
func verifyPackage(raw []byte) error {
	pkg, plugin, err := loadPackage(raw)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := plugin.SelfTest(ctx); err != nil {
		return fmt.Errorf("selftest: %w", err)
	}
	signer := "unsigned"
	if pkg.Signer != "" {
		signer = "signed by " + pkg.Signer
	}
	fmt.Fprintf(os.Stdout, "ok  %s@%s (%s), %d fixtures, %s\n", pkg.Manifest.ID, pkg.Manifest.Version, pkg.Manifest.Kind, len(pkg.Fixtures), signer)
	fmt.Fprintf(os.Stdout, "sha256 %s\n", pkg.SHA256)
	if pkg.Manifest.Kind == jsplugin.KindSiteProvider {
		writeSiteProviderFixtures(pkg.Fixtures)
	}
	return nil
}

// writeSiteProviderFixtures lists each sample with its name and hook. Other
// kinds keep the summary line only.
func writeSiteProviderFixtures(fixtures []jsplugin.Fixture) {
	if len(fixtures) == 0 {
		return
	}
	ordered := append([]jsplugin.Fixture(nil), fixtures...)
	sort.Slice(ordered, func(i, j int) bool {
		return fixtureSortKey(ordered[i]) < fixtureSortKey(ordered[j])
	})
	for _, fixture := range ordered {
		file := fixture.File
		if file == "" {
			file = fixture.Name
		}
		fmt.Fprintf(os.Stdout, "  %s (%s) hook=%s\n", file, fixture.Name, fixture.Hook)
	}
}

func fixtureSortKey(fixture jsplugin.Fixture) string {
	if fixture.File != "" {
		return fixture.File
	}
	return fixture.Name
}
