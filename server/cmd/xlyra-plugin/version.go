package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"xlyra/server/internal/jsplugin"
	"xlyra/server/internal/version"
)

func runVersion() {
	fmt.Fprintf(os.Stdout, "xlyra-plugin %s\n", version.Current().Version)
	fmt.Fprintf(os.Stdout, "apiVersion %d\n", jsplugin.HookAPIVersion)
	fmt.Fprintf(os.Stdout, "hostApi %d\n", jsplugin.HostAPIVersion)
	fmt.Fprintf(os.Stdout, "moejs %s\n", moejsVersion())
}

func moejsVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, dep := range info.Deps {
		if dep.Path == "github.com/Yachiyo-5i/moejs" {
			return dep.Version
		}
	}
	return "unknown"
}
