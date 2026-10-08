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
	fmt.Fprintln(w, "KIND\tFAMILY\tHOOKS\tCREDENTIAL\tCONNECTED")
	for _, info := range jsplugin.SupportedKinds() {
		connected := "yes"
		if !info.Connected {
			connected = "not yet"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", info.Name, info.Family, strings.Join(info.Hooks, ", "), info.Credential, connected)
	}
	_ = w.Flush()
	fmt.Fprintln(os.Stdout, "\nA kind that is not connected yet can be built, tested, packed and uploaded, but xLyra cannot enable it.")
}
