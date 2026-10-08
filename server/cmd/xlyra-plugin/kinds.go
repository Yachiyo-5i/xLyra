package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"xlyra/server/internal/jsplugin"
)

// runKinds lists the plugin kinds this build understands, so a developer can
// pick the one that fits instead of guessing.
func runKinds() {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "KIND\tFAMILY\tHOOKS\tCREDENTIAL\tTAKES EFFECT")
	anyOffline := false
	for _, info := range jsplugin.SupportedKinds() {
		effect := bindingText(info.Binding)
		if !info.Connected {
			effect = "not connected yet"
			anyOffline = true
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", info.Name, info.Family, strings.Join(info.Hooks, ", "), info.Credential, effect)
	}
	_ = w.Flush()
	if anyOffline {
		fmt.Fprintln(os.Stdout, "\nA kind that is not connected yet can be built, tested, packed and uploaded, but xLyra cannot enable it.")
	}
}

// bindingText says what an admin has to do for an enabled plugin to be used.
func bindingText(binding string) string {
	switch binding {
	case "site":
		return "enable, then bind to a site"
	case "global":
		return "enable (applies to every site)"
	case "quota":
		return "enable, then bind to a site's quota probe"
	case "slug":
		return "enable, then assign a /v1/plugins/<slug> path"
	}
	return binding
}
