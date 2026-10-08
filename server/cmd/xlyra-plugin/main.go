package main

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"flag"
	"fmt"
	"os"

	"xlyra/server/internal/jsplugin"
)

//go:generate go run ../../internal/jsplugin/gentypes -out templates/types/xlyra.d.ts -out ../../../sdk/js/index.d.ts

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "init":
		runInit(os.Args[2:])
	case "build":
		runBuild(os.Args[2:])
	case "run":
		runRun(os.Args[2:])
	case "test":
		runTest(os.Args[2:])
	case "pack":
		runPack(os.Args[2:])
	case "verify":
		runVerify(os.Args[2:])
	case "version", "--version", "-version":
		runVersion()
	case "keygen":
		runKeygen(os.Args[2:])
	case "sign":
		runSign(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage:\n")
	fmt.Fprintf(os.Stderr, "  xlyra-plugin init [--kind quota_probe|protocol] [--id ID] [--name NAME] <dir>\n")
	fmt.Fprintf(os.Stderr, "  xlyra-plugin build [--out dist] [dir]\n")
	fmt.Fprintf(os.Stderr, "  xlyra-plugin run --key-env VAR [--base-url URL] [--site-type T] [dir]\n")
	fmt.Fprintf(os.Stderr, "  xlyra-plugin test [--watch] [dir]\n")
	fmt.Fprintf(os.Stderr, "  xlyra-plugin pack [--sign key.pem] [--out file.xlp] [dir]\n")
	fmt.Fprintf(os.Stderr, "  xlyra-plugin verify <package.xlp>\n")
	fmt.Fprintf(os.Stderr, "  xlyra-plugin --version\n")
	fmt.Fprintf(os.Stderr, "  xlyra-plugin keygen [--out key.pem]\n")
	fmt.Fprintf(os.Stderr, "  xlyra-plugin sign <package.xlp> --key key.pem [--out signed.xlp]\n")
}

func runKeygen(args []string) {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	out := fs.String("out", "plugin-signing-key.pem", "output private key path")
	_ = fs.Parse(args)

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		fatal(err)
	}
	block := &pem.Block{Type: "PRIVATE KEY", Bytes: der}
	if err := os.WriteFile(*out, pem.EncodeToMemory(block), 0o600); err != nil {
		fatal(err)
	}
	fmt.Fprintf(os.Stderr, "Wrote private key to %s\n", *out)
	fmt.Fprintf(os.Stdout, "public_key=%s\n", base64.StdEncoding.EncodeToString(pub))
	fmt.Fprintf(os.Stdout, "fingerprint=%s\n", jsplugin.PublicKeyFingerprint(pub))
}

func runSign(args []string) {
	fs := flag.NewFlagSet("sign", flag.ExitOnError)
	keyPath := fs.String("key", "", "ed25519 private key PEM path")
	outPath := fs.String("out", "", "output package path (default: overwrite input)")
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		fatal(fmt.Errorf("package path is required"))
	}
	inPath := fs.Arg(0)
	if *keyPath == "" {
		fatal(fmt.Errorf("--key is required"))
	}
	priv, err := loadPrivateKey(*keyPath)
	if err != nil {
		fatal(err)
	}
	raw, err := os.ReadFile(inPath)
	if err != nil {
		fatal(err)
	}
	signed, err := jsplugin.AttachSignature(raw, priv)
	if err != nil {
		fatal(err)
	}
	target := *outPath
	if target == "" {
		target = inPath
	}
	if err := os.WriteFile(target, signed, 0o644); err != nil {
		fatal(err)
	}
	fmt.Fprintf(os.Stderr, "Wrote signed package to %s\n", target)
}

func loadPrivateKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("invalid PEM in %s", path)
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	priv, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key is not ed25519")
	}
	return priv, nil
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "xlyra-plugin: %v\n", err)
	os.Exit(1)
}
