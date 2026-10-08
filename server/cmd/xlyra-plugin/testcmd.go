package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

func runTest(args []string) {
	fs := flag.NewFlagSet("test", flag.ExitOnError)
	watch := fs.Bool("watch", false, "rerun when files under the project change")
	_ = fs.Parse(args)
	dir := "."
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}
	if !*watch {
		if !testOnce(dir) {
			os.Exit(1)
		}
		return
	}
	var last string
	for {
		stamp := projectStamp(dir)
		if stamp != last {
			last = stamp
			fmt.Fprintf(os.Stdout, "\n--- %s ---\n", time.Now().Format("15:04:05"))
			testOnce(dir)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// testOnce builds the project and runs its fixtures. It reports whether all passed.
func testOnce(dir string) bool {
	built, err := buildProject(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "xlyra-plugin: %v\n", err)
		return false
	}
	_, pkg, plugin, err := built.assemble()
	if err != nil {
		fmt.Fprintf(os.Stderr, "xlyra-plugin: %v\n", err)
		return false
	}
	var out bytes.Buffer
	failed := runFixtures(plugin, built.Fixtures, &out)
	fmt.Fprintf(os.Stdout, "%s@%s\n%s", pkg.Manifest.ID, pkg.Manifest.Version, out.String())
	fmt.Fprintf(os.Stdout, "%d passed, %d failed\n", len(built.Fixtures)-failed, failed)
	return failed == 0
}

// projectStamp fingerprints the files that feed a build.
func projectStamp(dir string) string {
	var parts []string
	for _, pattern := range []string{"manifest.json", "src/*", "fixtures/*.json", "tsconfig.json"} {
		matches, _ := filepath.Glob(filepath.Join(dir, pattern))
		for _, match := range matches {
			if info, err := os.Stat(match); err == nil {
				parts = append(parts, fmt.Sprintf("%s:%d:%d", match, info.Size(), info.ModTime().UnixNano()))
			}
		}
	}
	sort.Strings(parts)
	return fmt.Sprint(parts)
}
