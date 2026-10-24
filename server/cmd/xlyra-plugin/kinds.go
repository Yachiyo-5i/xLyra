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
		effect := scopeText(info.Scope)
		if !info.Connected {
			effect = "not connected yet"
			anyOffline = true
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", info.Name, info.Family, formatHooks(info), info.Credential, effect)
	}
	_ = w.Flush()
	if anyOffline {
		fmt.Fprintln(os.Stdout, "\nA kind that is not connected yet can be built, tested, packed and uploaded, but xLyra cannot enable it.")
	}
}

// scopeText says what an admin has to do for an enabled plugin to be used.
func scopeText(scope jsplugin.Scope) string {
	switch scope {
	case jsplugin.ScopeSite:
		return "enable, then bind to a site"
	case jsplugin.ScopeGlobal:
		return "enable (applies to every site)"
	case jsplugin.ScopeEndpoint:
		return "enable, then assign a /v1/plugins/<slug> path"
	case jsplugin.ScopeSiteType:
		return "enable after a trusted signature and host confirmation (applies to its site type)"
	}
	return string(scope)
}

func formatHooks(info jsplugin.KindInfo) string {
	hooks := strings.Join(info.Hooks, ", ")
	if len(info.OptionalHooks) == 0 {
		return hooks
	}
	optional := "(optional: " + strings.Join(info.OptionalHooks, ", ") + ")"
	if hooks == "" {
		return optional
	}
	return hooks + " " + optional
}
