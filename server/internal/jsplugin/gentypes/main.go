// Command gentypes writes the plugin SDK .d.ts generated from the Go contract.
package main

import (
	"flag"
	"log"
	"os"

	"xlyra/server/internal/jsplugin"
)

type outputs []string

func (o *outputs) String() string     { return "" }
func (o *outputs) Set(v string) error { *o = append(*o, v); return nil }

func main() {
	var out outputs
	flag.Var(&out, "out", "output file (repeatable)")
	flag.Parse()
	text, err := jsplugin.TypeDeclarations()
	if err != nil {
		log.Fatal(err)
	}
	for _, path := range out {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			log.Fatal(err)
		}
	}
}
