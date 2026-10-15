package main

import (
	"bytes"
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xlyra/server/internal/jsplugin"
)

func newProject(t *testing.T, kind, id string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), id)
	if err := scaffold(dir, kind, id, ""); err != nil {
		t.Fatal(err)
	}
	return dir
}

// A freshly scaffolded project must pass its own fixtures and the server's
// upload checks, for every kind this build supports. A new kind without a
// working template fails here.
func TestScaffoldedProjectsPassVerify(t *testing.T) {
	for _, info := range jsplugin.SupportedKinds() {
		kind := info.Name
		t.Run(kind, func(t *testing.T) {
			dir := newProject(t, kind, "acme-"+strings.ReplaceAll(kind, "_", "-"))
			built, err := buildProject(dir)
			if err != nil {
				t.Fatal(err)
			}
			raw, _, plugin, err := built.assemble()
			if err != nil {
				t.Fatal(err)
			}
			var failed int
			failed = runFixtures(plugin, built.Fixtures, new(bytes.Buffer))
			if failed != 0 {
				t.Fatalf("%d fixtures failed", failed)
			}
			if err := verifyPackage(raw); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBuildRejectsExternalAndDynamicImports(t *testing.T) {
	for name, line := range map[string]string{
		"bare":    `import x from "lodash"; export const z = x;`,
		"dynamic": `export const z = () => import("./other");`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := newProject(t, jsplugin.KindQuotaProbe, "imports-"+name)
			entry := filepath.Join(dir, "src", "index.ts")
			raw, _ := os.ReadFile(entry)
			if err := os.WriteFile(entry, append([]byte(line+"\n"), raw...), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := buildProject(dir); err == nil || !strings.Contains(err.Error(), "not allowed") {
				t.Fatalf("err = %v, want an import rejection", err)
			}
		})
	}
}

func TestFailingFixtureIsReported(t *testing.T) {
	dir := newProject(t, jsplugin.KindQuotaProbe, "failing-fixture")
	path := filepath.Join(dir, "fixtures", "example.json")
	raw, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte(strings.Replace(string(raw), `"remaining": 12.5`, `"remaining": 99`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	built, err := buildProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, _, plugin, err := built.assemble()
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if failed := runFixtures(plugin, built.Fixtures, &out); failed != 1 {
		t.Fatalf("failed = %d, want 1", failed)
	}
	if !strings.Contains(out.String(), "remaining") {
		t.Fatalf("output does not name the mismatch:\n%s", out.String())
	}
}

func TestSignedPackageVerifiesAndTamperingFails(t *testing.T) {
	dir := newProject(t, jsplugin.KindQuotaProbe, "signed-plugin")
	built, err := buildProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, _, _, err := built.assemble()
	if err != nil {
		t.Fatal(err)
	}
	_, priv, _ := ed25519.GenerateKey(nil)
	signed, err := jsplugin.AttachSignature(raw, priv)
	if err != nil {
		t.Fatal(err)
	}
	pkg, _, err := loadPackage(signed)
	if err != nil || pkg.Signer == "" {
		t.Fatalf("signer = %q, err = %v", pkg.Signer, err)
	}
	if _, _, err := loadPackage(signed[:len(signed)/2]); err == nil {
		t.Fatal("truncated package was accepted")
	}
}

// build must keep .xlp files that pack wrote into dist/.
func TestWriteTreeKeepsPackages(t *testing.T) {
	root := t.TempDir()
	keep := filepath.Join(root, "x.xlp")
	if err := os.WriteFile(keep, []byte("pkg"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeTree(root, map[string][]byte{"plugin.js": []byte("a"), "fixtures/f.json": []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	if err := writeTree(root, map[string][]byte{"plugin.js": []byte("b")}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("xlp was removed")
	}
	if _, err := os.Stat(filepath.Join(root, "fixtures")); err == nil {
		t.Fatal("stale fixtures were kept")
	}
}

func TestInitRefusesNonEmptyDirAndReservedID(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := scaffold(dir, jsplugin.KindQuotaProbe, "valid-id", ""); err == nil {
		t.Fatal("non-empty dir was accepted")
	}
	if err := scaffold(filepath.Join(t.TempDir(), "p"), jsplugin.KindQuotaProbe, "xlyra.mine", ""); err == nil {
		t.Fatal("xlyra. prefix was accepted")
	}
}

// The SDK types are generated from the Go contract (go generate ./cmd/xlyra-plugin).
// Both copies must match the generator output, so a contract change cannot ship
// without regenerating them.
func TestSDKTypesInSync(t *testing.T) {
	want, err := jsplugin.TypeDeclarations()
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := templates.ReadFile("templates/types/xlyra.d.ts")
	if err != nil {
		t.Fatal(err)
	}
	published, err := os.ReadFile(filepath.Join("..", "..", "..", "sdk", "js", "index.d.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string][]byte{"templates/types/xlyra.d.ts": embedded, "sdk/js/index.d.ts": published} {
		if string(got) != want {
			t.Errorf("%s is stale; run `go generate ./cmd/xlyra-plugin` in server/", name)
		}
	}
}
