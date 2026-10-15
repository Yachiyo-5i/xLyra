package main

import (
	"log"

	"xlyra/server/internal/jsplugin/builtingen"
)

func main() {
	if err := builtingen.WriteBuiltins("builtin"); err != nil {
		log.Fatal(err)
	}
}
