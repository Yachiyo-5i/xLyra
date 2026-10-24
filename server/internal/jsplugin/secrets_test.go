package jsplugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"
	"testing/quick"
)

func TestParseSecretPath(t *testing.T) {
	valid := []struct {
		in   string
		want string
	}{
		{"a", "a"},
		{"data.accessToken", "data.accessToken"},
		{"items[0].token", "items[0].token"},
		{"a.b[2].c", "a.b[2].c"},
		{"items[0][1].token", "items[0][1].token"},
		{"a[0]", "a[0]"},
		{"items[0][1][2]", "items[0][1][2]"},
		{"A_B-c.d0[12]", "A_B-c.d0[12]"},
		{"a.b_c", "a.b_c"},
		{"n0.n1[0].n2", "n0.n1[0].n2"},
		{"a.b[10].c[0]", "a.b[10].c[0]"},
		{"k0.k1", "k0.k1"},
		// Leading zeros are still a non-negative integer.
		{"a[01]", "a[1]"},
		{"a[00]", "a[0]"},
	}
	for _, tc := range valid {
		got, err := ParseSecretPath(tc.in)
		if err != nil {
			t.Fatalf("ParseSecretPath(%q): %v", tc.in, err)
		}
		if got.String() != tc.want {
			t.Fatalf("ParseSecretPath(%q) = %q, want %q", tc.in, got.String(), tc.want)
		}
	}

	invalid := []string{
		"",
		".",
		".a",
		"a.",
		"a..b",
		"..",
		"a[",
		"a[0",
		"a[-1]",
		"a[+1]",
		"a[1.2]",
		"a[]",
		"a[0]]",
		"a[[0]]",
		"a[b]",
		"*",
		"a.*.b",
		"items[*]",
		"a[0]b",
		"a.[0]",
		"[0]",
		"[0].a",
		"a[0].",
		"a..",
		"数据.token",
		"a.b.",
		"a b.c",
		"a[-0]",
		"a[999999999999999999999]",
		"a['b']",
		`a["b"]`,
		"a[0][1].",
		".[0]",
		"a..[0]",
		"a[ 0]",
		" ",
		"a.",
		"*",
		"items[0].",
		"a[0x1]",
		"a..b.c",
		"..a",
		"a[1][2].",
	}
	for _, raw := range invalid {
		_, err := ParseSecretPath(raw)
		if !errors.Is(err, ErrInvalidSecretPath) {
			t.Fatalf("ParseSecretPath(%q) err = %v, want ErrInvalidSecretPath", raw, err)
		}
	}
}

func TestSecretBagSetGetHasFields(t *testing.T) {
	bag := NewSecretBag()
	if bag.Has("access_token") || bag.Fields() != nil {
		t.Fatalf("empty bag = %+v", bag.Fields())
	}
	if v, ok := bag.Get("access_token"); ok || v != "" {
		t.Fatalf("Get empty = %q %v", v, ok)
	}
	bag.Set("refresh_token", "refresh-secret-value")
	bag.Set("access_token", "access-secret-value")
	if !bag.Has("access_token") {
		t.Fatal("missing access_token")
	}
	got, ok := bag.Get("access_token")
	if !ok || got != "access-secret-value" {
		t.Fatalf("Get = %q %v", got, ok)
	}
	if fields := bag.Fields(); strings.Join(fields, ",") != "access_token,refresh_token" {
		t.Fatalf("Fields = %v", fields)
	}
	bag.Set("access_token", "rotated-secret-value")
	got, _ = bag.Get("access_token")
	if got != "rotated-secret-value" {
		t.Fatalf("rotated = %q", got)
	}
	// The previous value stays scrubbable after Set replaces it.
	scrubbed := bag.Scrub("access-secret-value and rotated-secret-value")
	if strings.Contains(scrubbed, "access-secret-value") || strings.Contains(scrubbed, "rotated-secret-value") {
		t.Fatalf("rotation leak: %s", scrubbed)
	}

	var none *SecretBag
	if none.Has("access_token") || none.Fields() != nil {
		t.Fatal("nil bag")
	}
	if v, ok := none.Get("access_token"); ok || v != "" {
		t.Fatal("nil Get")
	}
	if none.Scrub("access-secret-value") != "access-secret-value" {
		t.Fatal("nil Scrub")
	}
}

func TestScrubOverlappingAndShort(t *testing.T) {
	bag := NewSecretBag()
	bag.Set("short3", "abc")
	bag.Set("emoji", "😀😀")
	bag.Set("four", "abcd")
	bag.Set("long", "abcdefghij")
	bag.Set("emoji4", "😀😀😀😀")

	if got := bag.Scrub("abc 😀😀 abcd xxabcdefghijyy"); got != "abc 😀😀 [redacted] xx[redacted]yy" {
		t.Fatalf("scrub = %q", got)
	}
	if strings.Contains(bag.Scrub("xxabcdefghijyy"), "efghij") {
		t.Fatal("short needle split the longer secret")
	}
	if !strings.Contains(bag.Scrub("keep 😀😀😀😀"), secretScrubMark) || strings.Contains(bag.Scrub("keep 😀😀😀😀"), "😀😀😀😀") {
		t.Fatalf("emoji secret = %q", bag.Scrub("keep 😀😀😀😀"))
	}
	if bag.Scrub("") != "" {
		t.Fatal("empty")
	}
}

func TestScrubJSONEscape(t *testing.T) {
	secret := "say \"hi\" \\ path 😀 </script>"
	bag := NewSecretBag()
	bag.Set("token", secret)
	frag := jsonStringFragment(secret)
	if frag == secret {
		t.Fatalf("expected JSON escaping, fragment %q", frag)
	}
	for _, sample := range []string{
		"prefix " + secret + " suffix",
		"prefix " + frag + " suffix",
	} {
		got := bag.Scrub(sample)
		if strings.Contains(got, secret) || strings.Contains(got, frag) {
			t.Fatalf("scrubbed %q still leaks: %s", sample, got)
		}
		if !strings.Contains(got, secretScrubMark) {
			t.Fatalf("scrubbed %q = %s", sample, got)
		}
	}
}

func TestScrubJSONCopy(t *testing.T) {
	bag := NewSecretBag()
	bag.Set("token", "super-secret-token")
	in := map[string]any{
		"super-secret-token": "keep",
		"note":               "super-secret-token",
		"arr":                []any{"super-secret-token", map[string]any{"n": 1}},
	}
	got := bag.ScrubJSON(in).(map[string]any)
	in["note"] = "changed"
	in["super-secret-token"] = "changed-key-value"
	if got["note"] != secretScrubMark {
		t.Fatalf("note = %#v", got["note"])
	}
	if _, ok := got["super-secret-token"]; ok {
		t.Fatalf("key survived: %#v", got)
	}
	if _, ok := got[secretScrubMark]; !ok {
		t.Fatalf("scrubbed key missing: %#v", got)
	}
	arr := got["arr"].([]any)
	if arr[0] != secretScrubMark {
		t.Fatalf("arr = %#v", arr)
	}
	if bag.ScrubJSON(nil) != nil {
		t.Fatal("nil value")
	}
	if NewSecretBag().ScrubJSON("plain") != "plain" {
		t.Fatal("empty bag changed text")
	}
}

func TestScrubErrorIsAs(t *testing.T) {
	bag := NewSecretBag()
	bag.Set("token", "super-secret-token-value")

	base := fmt.Errorf("open super-secret-token-value: %w", os.ErrNotExist)
	scrubbed := bag.ScrubError(base)
	if !errors.Is(scrubbed, os.ErrNotExist) {
		t.Fatalf("Is lost: %v", scrubbed)
	}
	if strings.Contains(scrubbed.Error(), "super-secret-token-value") {
		t.Fatalf("Error leaked: %s", scrubbed.Error())
	}
	if !strings.Contains(scrubbed.Error(), secretScrubMark) {
		t.Fatalf("Error = %s", scrubbed.Error())
	}

	pathErr := &os.PathError{Op: "open", Path: "dir/super-secret-token-value", Err: os.ErrPermission}
	wrapped := bag.ScrubError(pathErr)
	var target *os.PathError
	if !errors.As(wrapped, &target) || target != pathErr {
		t.Fatalf("As = %#v err=%v", target, wrapped)
	}
	if strings.Contains(wrapped.Error(), "super-secret-token-value") {
		t.Fatalf("As Error leaked: %s", wrapped.Error())
	}
	// Structured fields stay intact so errors.As semantics do not change.
	if target.Path != pathErr.Path {
		t.Fatalf("Path rewritten: %s", target.Path)
	}

	plain := errors.New("nothing secret")
	if bag.ScrubError(plain) != plain {
		t.Fatal("unchanged error was wrapped")
	}
	if bag.ScrubError(nil) != nil {
		t.Fatal("nil")
	}
	empty := NewSecretBag()
	leaky := errors.New("super-secret-token-value")
	if empty.ScrubError(leaky) != leaky {
		t.Fatal("empty bag wrapped")
	}
	if (*SecretBag)(nil).ScrubError(leaky) != leaky {
		t.Fatal("nil bag wrapped")
	}
}

func TestRedactResponseNested(t *testing.T) {
	body := []byte(`{"data":{"accessToken":"access-secret-value","refreshToken":"refresh-secret-value"},"items":[{"token":"item-secret-value","n":1}]}`)
	rules := map[string]string{
		"data.accessToken":  "access_token",
		"data.refreshToken": "refresh_token",
		"items[0].token":    "item_token",
	}
	bag := NewSecretBag()
	out, js, err := RedactResponse("application/json; charset=utf-8", body, rules, bag)
	if err != nil {
		t.Fatal(err)
	}
	assertConsistent(t, out, js)
	assertNoSecret(t, out, js, "access-secret-value", "refresh-secret-value", "item-secret-value")
	if mustStringAt(t, js, "data.accessToken") != SecretRedactedPlaceholder {
		t.Fatalf("access = %#v", js)
	}
	if mustStringAt(t, js, "data.refreshToken") != SecretRedactedPlaceholder {
		t.Fatalf("refresh = %#v", js)
	}
	if mustStringAt(t, js, "items[0].token") != SecretRedactedPlaceholder {
		t.Fatalf("item = %#v", js)
	}
	if v, _ := bag.Get("access_token"); v != "access-secret-value" {
		t.Fatalf("bag access = %q", v)
	}
	if v, _ := bag.Get("refresh_token"); v != "refresh-secret-value" {
		t.Fatalf("bag refresh = %q", v)
	}
	if v, _ := bag.Get("item_token"); v != "item-secret-value" {
		t.Fatalf("bag item = %q", v)
	}
	if !bytes.Equal(body, []byte(`{"data":{"accessToken":"access-secret-value","refreshToken":"refresh-secret-value"},"items":[{"token":"item-secret-value","n":1}]}`)) {
		t.Fatal("input body was modified")
	}
}

func TestRedactResponseSameFieldAndMissing(t *testing.T) {
	body := []byte(`{"b":"bbbb-secret-value","a":"aaaa-secret-value","c":"","d":{"n":1}}`)
	rules := map[string]string{
		"a":       "token",
		"b":       "token",
		"c":       "token",
		"missing": "token",
		"gone.x":  "other",
	}
	bag := NewSecretBag()
	out, js, err := RedactResponse("application/json", body, rules, bag)
	if err != nil {
		t.Fatal(err)
	}
	assertConsistent(t, out, js)
	assertNoSecret(t, out, js, "aaaa-secret-value", "bbbb-secret-value")
	// Lexicographic path order: "a" wins over "b". Empty and missing do not.
	if v, ok := bag.Get("token"); !ok || v != "aaaa-secret-value" {
		t.Fatalf("token = %q ok=%v", v, ok)
	}
	if bag.Has("other") {
		t.Fatal("missing path wrote the bag")
	}
	if mustStringAt(t, js, "a") != SecretRedactedPlaceholder || mustStringAt(t, js, "b") != SecretRedactedPlaceholder {
		t.Fatalf("placeholders = %#v", js)
	}
	if mustStringAt(t, js, "c") != "" {
		t.Fatalf("empty string = %#v", js)
	}

	// A pre-existing bag value stays; the new hit is still removed.
	bag = NewSecretBag()
	bag.Set("token", "older-secret-value")
	body = []byte(`{"a":"newer-secret-value","echo":"newer-secret-value"}`)
	out, js, err = RedactResponse("", body, map[string]string{"a": "token"}, bag)
	if err != nil {
		t.Fatal(err)
	}
	assertNoSecret(t, out, js, "newer-secret-value", "older-secret-value")
	if v, _ := bag.Get("token"); v != "older-secret-value" {
		t.Fatalf("kept = %q", v)
	}
	if mustStringAt(t, js, "a") != SecretRedactedPlaceholder {
		t.Fatalf("a = %#v", js)
	}
}

func TestRedactResponseNotString(t *testing.T) {
	cases := []string{
		`{"data":{"accessToken":1}}`,
		`{"data":{"accessToken":null}}`,
		`{"data":{"accessToken":{"raw":"x"}}}`,
		`{"data":{"accessToken":["x"]}}`,
		`{"data":{"accessToken":true}}`,
	}
	for _, body := range cases {
		bag := NewSecretBag()
		out, js, err := RedactResponse("application/json", []byte(body), map[string]string{"data.accessToken": "access_token"}, bag)
		if !errors.Is(err, ErrSecretValueNotString) || out != nil || js != nil {
			t.Fatalf("body %s => out=%s js=%v err=%v", body, out, js, err)
		}
		if bag.Has("access_token") {
			t.Fatal("bag written on type error")
		}
		if strings.Contains(err.Error(), "accessToken") && strings.Contains(err.Error(), "secret-") {
			t.Fatalf("error leaked value: %v", err)
		}
	}

	// Traversal that stops early is a missing path, not a type error.
	for _, body := range []string{`{"data":null}`, `{"data":[1]}`, `{"data":{}}`} {
		bag := NewSecretBag()
		out, js, err := RedactResponse("application/json", []byte(body), map[string]string{"data.accessToken": "access_token"}, bag)
		if err != nil || bag.Has("access_token") {
			t.Fatalf("body %s => out=%s err=%v has=%v", body, out, err, bag.Has("access_token"))
		}
		assertConsistent(t, out, js)
	}

	bag := NewSecretBag()
	_, _, err := RedactResponse("application/json", []byte(`{"data":null}`), map[string]string{"data": "access_token"}, bag)
	if !errors.Is(err, ErrSecretValueNotString) {
		t.Fatalf("null at path: %v", err)
	}
}

func TestRedactResponseNotObjectAndNotJSON(t *testing.T) {
	secret := "html-secret-token-value"
	rules := map[string]string{"data.accessToken": "access_token"}
	cases := []struct {
		ct   string
		body string
		want error
	}{
		{"application/json", `[{"token":"` + secret + `"}]`, ErrSecretRedactNotObject},
		{"application/json", `"` + secret + `"`, ErrSecretRedactNotObject},
		{"application/json", `42`, ErrSecretRedactNotObject},
		{"application/json", `true`, ErrSecretRedactNotObject},
		{"application/json", `null`, ErrSecretRedactNotObject},
		{"text/html", "<html>" + secret + "</html>", ErrSecretRedactNotJSON},
		{"text/html; charset=utf-8", "<html>" + secret + "</html>", ErrSecretRedactNotJSON},
		{"text/plain", `{"data":{"accessToken":"` + secret + `"}}`, ErrSecretRedactNotJSON},
		{"application/octet-stream", secret, ErrSecretRedactNotJSON},
		{"not a type", secret, ErrSecretRedactNotJSON},
		{"application/json", "<html>" + secret + "</html>", ErrSecretRedactNotJSON},
		{"", "<html>" + secret + "</html>", ErrSecretRedactNotJSON},
		{"application/json", `{"a":1}{"b":2}`, ErrSecretRedactNotJSON},
	}
	for _, tc := range cases {
		out, js, err := RedactResponse(tc.ct, []byte(tc.body), rules, NewSecretBag())
		if !errors.Is(err, tc.want) || out != nil || js != nil {
			t.Fatalf("ct=%q body=%q => out=%s js=%v err=%v", tc.ct, tc.body, out, js, err)
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error contains secret: %v", err)
		}
	}

	if _, _, err := RedactResponse("application/json", []byte(`{"a":"x"}`), rules, nil); !errors.Is(err, ErrNilSecretBag) {
		t.Fatalf("nil bag: %v", err)
	}
	if _, _, err := RedactResponse("application/json", []byte(`{"a":"secret-value-here"}`), map[string]string{"a": "Access"}, NewSecretBag()); !errors.Is(err, ErrSecretFieldName) {
		t.Fatalf("field name: %v", err)
	}
	if _, _, err := RedactResponse("application/json", []byte(`{"a":"secret-value-here"}`), map[string]string{"a..b": "access_token"}, NewSecretBag()); !errors.Is(err, ErrInvalidSecretPath) {
		t.Fatalf("bad path: %v", err)
	}
}

func TestRedactResponseEmptyRules(t *testing.T) {
	pretty := []byte("{\n  \"n\": 1\n}")
	out, js, err := RedactResponse("text/html", pretty, nil, nil)
	if err != nil || !bytes.Equal(out, pretty) {
		t.Fatalf("out=%s err=%v", out, err)
	}
	n, ok := js.(map[string]any)["n"].(float64)
	if !ok || n != 1 {
		t.Fatalf("json = %#v (%T)", js, js.(map[string]any)["n"])
	}

	html := []byte("<html>secret-looking</html>")
	out, js, err = RedactResponse("", html, map[string]string{}, NewSecretBag())
	if err != nil || !bytes.Equal(out, html) || js != nil {
		t.Fatalf("html out=%s js=%#v err=%v", out, js, err)
	}

	array := []byte(`[1,2]`)
	out, js, err = RedactResponse("application/json", array, nil, nil)
	if err != nil || !bytes.Equal(out, array) {
		t.Fatal(err)
	}
	if _, ok := js.([]any); !ok {
		t.Fatalf("array json = %#v", js)
	}
}

func TestRedactResponseBigInt(t *testing.T) {
	body := []byte(`{"id":9007199254740993,"data":{"accessToken":"super-secret-token"},"nums":[-9007199254740993]}`)
	bag := NewSecretBag()
	out, js, err := RedactResponse("Application/JSON", body, map[string]string{"data.accessToken": "access_token"}, bag)
	if err != nil {
		t.Fatal(err)
	}
	assertConsistent(t, out, js)
	assertNoSecret(t, out, js, "super-secret-token")
	if !bytes.Contains(out, []byte("9007199254740993")) || bytes.Contains(out, []byte("9007199254740992")) {
		t.Fatalf("precision: %s", out)
	}
	if !bytes.Contains(out, []byte("-9007199254740993")) {
		t.Fatalf("negative precision: %s", out)
	}
	id, ok := walk(js, mustPath(t, "id"), nil, false)
	if !ok || id.(json.Number).String() != "9007199254740993" {
		t.Fatalf("id = %#v", id)
	}
}

func TestRedactResponseUnicodeAndEscapes(t *testing.T) {
	secret := "say \"hi\" \\ path 😀 </script>"
	payload := map[string]any{
		"data": map[string]any{
			"accessToken": secret,
			"copy":        secret,
			"note":        "prefix " + secret + " suffix",
		},
		"arr": []any{secret},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	bag := NewSecretBag()
	out, js, err := RedactResponse("application/problem+json", body, map[string]string{"data.accessToken": "access_token"}, bag)
	if err != nil {
		t.Fatal(err)
	}
	assertConsistent(t, out, js)
	assertNoSecret(t, out, js, secret)
	if bytes.Contains(out, []byte("</script>")) || bytes.Contains(out, []byte(`\u003c/script\u003e`)) {
		t.Fatalf("script leaked: %s", out)
	}
	if mustStringAt(t, js, "data.accessToken") != SecretRedactedPlaceholder {
		t.Fatalf("declared = %#v", js)
	}
	if mustStringAt(t, js, "data.copy") != secretScrubMark {
		t.Fatalf("copy = %#v", js)
	}
	if mustStringAt(t, js, "arr[0]") != secretScrubMark {
		t.Fatalf("arr = %#v", js)
	}
	if note := mustStringAt(t, js, "data.note"); note != "prefix "+secretScrubMark+" suffix" {
		t.Fatalf("note = %q", note)
	}
}

func TestRedactResponseLeakScan(t *testing.T) {
	// The declared path is data.accessToken. The same secret is also placed
	// where no rule points: a nested field, an array, and an object key.
	// Paths select values, not keys, so a key cannot be extracted. Scrub
	// still erases the secret inside that key. If two keys scrub to the
	// same string, the lexicographically later original key is kept and
	// the secret is in neither.
	const secret = "leak-scan-secret-value"
	payload := map[string]any{
		"data": map[string]any{
			"accessToken": secret,
			"raw":         map[string]any{"token": secret},
			"list":        []any{map[string]any{"v": []any{secret, "safe"}}},
			"note":        "Bearer " + secret,
		},
		secret:        map[string]any{"kept": true},
		"x-" + secret: "side",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	bag := NewSecretBag()
	out, js, err := RedactResponse("application/json", body, map[string]string{"data.accessToken": "access_token"}, bag)
	if err != nil {
		t.Fatal(err)
	}
	assertConsistent(t, out, js)
	assertNoSecret(t, out, js, secret)
	if mustStringAt(t, js, "data.accessToken") != SecretRedactedPlaceholder {
		t.Fatalf("declared = %#v", js)
	}
	if mustStringAt(t, js, "data.raw.token") != secretScrubMark {
		t.Fatalf("undeclared = %#v", js)
	}
	if mustStringAt(t, js, "data.list[0].v[0]") != secretScrubMark {
		t.Fatalf("array = %#v", js)
	}
	if mustStringAt(t, js, "data.list[0].v[1]") != "safe" {
		t.Fatalf("safe = %#v", js)
	}
	if note := mustStringAt(t, js, "data.note"); note != "Bearer "+secretScrubMark {
		t.Fatalf("note = %q", note)
	}
	root := js.(map[string]any)
	if _, ok := root[secret]; ok {
		t.Fatal("secret key survived")
	}
	if _, ok := root["x-"+secret]; ok {
		t.Fatal("secret substring key survived")
	}
	kept, ok := root[secretScrubMark].(map[string]any)
	if !ok || kept["kept"] != true {
		t.Fatalf("scrubbed key = %#v", root[secretScrubMark])
	}
	if _, ok := root["x-"+secretScrubMark]; !ok {
		t.Fatalf("substring key = %#v", root)
	}
	if v, _ := bag.Get("access_token"); v != secret {
		t.Fatalf("bag = %q", v)
	}
}

func TestRedactResponseMarkerSubstringFailsClosed(t *testing.T) {
	// "redacted" is contained in both {{redacted}} and [redacted], so scrubbing
	// cannot remove it. The call fails closed and does not return the body.
	body := []byte(`{"data":{"accessToken":"redacted","echo":"redacted"}}`)
	out, js, err := RedactResponse("application/json", body, map[string]string{"data.accessToken": "access_token"}, NewSecretBag())
	if !errors.Is(err, ErrSecretRedactLeak) || out != nil || js != nil {
		t.Fatalf("out=%s js=%v err=%v", out, js, err)
	}
	if strings.Contains(err.Error(), "redacted") {
		t.Fatalf("error contains the secret: %v", err)
	}
}

func TestRedactResponsePropertyNoLeak(t *testing.T) {
	rng := rand.New(rand.NewSource(20261021))
	for i := 0; i < 40; i++ {
		body, rules, secrets := randomRedactCase(rng)
		bag := NewSecretBag()
		out, js, err := RedactResponse("application/json", body, rules, bag)
		if err != nil {
			t.Fatalf("case %d: %v\nbody %s", i, err, body)
		}
		assertConsistent(t, out, js)
		assertNoSecret(t, out, js, secrets...)
		if !bytes.Contains(out, []byte("9007199254740993")) {
			t.Fatalf("case %d lost precision: %s", i, out)
		}
		if bag.Has("not_here") {
			t.Fatalf("case %d missing path wrote bag", i)
		}
		if !bag.Has("field_0") {
			t.Fatalf("case %d did not keep field_0", i)
		}
	}
}

func TestScrubQuickNoSecretLeft(t *testing.T) {
	cfg := &quick.Config{MaxCount: 80, Rand: rand.New(rand.NewSource(1))}
	err := quick.Check(func(secret, prefix, suffix string) bool {
		if utf8RuneCount(secret) < minScrubRunes {
			return true
		}
		if strings.Contains(secretScrubMark, secret) || strings.Contains(secret, secretScrubMark) {
			return true
		}
		if strings.Contains(SecretRedactedPlaceholder, secret) {
			return true
		}
		bag := NewSecretBag()
		bag.Set("s", secret)
		got := bag.Scrub(prefix + secret + suffix)
		return !strings.Contains(got, secret)
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSubstituteSecrets(t *testing.T) {
	bag := NewSecretBag()
	bag.Set("access_token", "access-secret-value")
	bag.Set("refresh_token", "refresh-secret-value")
	allowed := map[string]bool{"access_token": true, "refresh_token": true}
	req := ProbeRequest{
		Method:  "POST",
		Path:    "/v1/chat",
		Headers: map[string]string{"Authorization": "Bearer {{secret.access_token}}", "Accept": "application/json"},
		Query:   map[string]string{"token": "{{secret.refresh_token}}", "q": "plain"},
		Body: map[string]any{
			"text": "tok {{secret.access_token}}",
			"arr":  []any{"{{secret.refresh_token}}", map[string]any{"n": "{{foo}} {{secret.access_token}}", "num": json.Number("7")}},
			"keep": "{{foo}}",
			"cred": "{{cred.access_token}}",
			"gap":  "{{ secret.access_token }}",
		},
	}
	out, err := SubstituteSecrets(req, bag, allowed)
	if err != nil {
		t.Fatal(err)
	}
	if out.Path != "/v1/chat" || out.Method != "POST" {
		t.Fatalf("path/method changed: %+v", out)
	}
	if strings.Contains(out.Path, "access-secret-value") || strings.Contains(out.Path, "evil.example") {
		t.Fatalf("path rewritten: %s", out.Path)
	}
	if out.Headers["Authorization"] != "Bearer access-secret-value" || out.Headers["Accept"] != "application/json" {
		t.Fatalf("headers = %#v", out.Headers)
	}
	if out.Query["token"] != "refresh-secret-value" || out.Query["q"] != "plain" {
		t.Fatalf("query = %#v", out.Query)
	}
	body := out.Body.(map[string]any)
	if body["text"] != "tok access-secret-value" {
		t.Fatalf("text = %#v", body["text"])
	}
	arr := body["arr"].([]any)
	if arr[0] != "refresh-secret-value" {
		t.Fatalf("arr0 = %#v", arr[0])
	}
	nested := arr[1].(map[string]any)
	if nested["n"] != "{{foo}} access-secret-value" || nested["num"] != json.Number("7") {
		t.Fatalf("nested = %#v", nested)
	}
	if body["keep"] != "{{foo}}" || body["cred"] != "{{cred.access_token}}" || body["gap"] != "{{ secret.access_token }}" {
		t.Fatalf("kept = %#v", body)
	}

	// A secret that looks like a URL stays in the query. Path and host are untouched.
	bag.Set("access_token", "https://evil.example/steal")
	req = ProbeRequest{Method: "GET", Path: "/api/v1/models", Query: map[string]string{"next": "{{secret.access_token}}"}}
	out, err = SubstituteSecrets(req, bag, allowed)
	if err != nil {
		t.Fatal(err)
	}
	if out.Path != "/api/v1/models" || out.Method != "GET" || out.Query["next"] != "https://evil.example/steal" {
		t.Fatalf("url shape = %+v", out)
	}

	stringBody := ProbeRequest{Method: "POST", Path: "/v1", Body: "Bearer {{secret.refresh_token}} {{foo}}"}
	bag.Set("refresh_token", "refresh-secret-value")
	out, err = SubstituteSecrets(stringBody, bag, allowed)
	if err != nil || out.Body != "Bearer refresh-secret-value {{foo}}" {
		t.Fatalf("string body = %#v %v", out.Body, err)
	}

	onlyFoo := ProbeRequest{Method: "GET", Path: "/v1", Body: "{{foo}}", Headers: map[string]string{"X-A": "{{notsecret}}"}}
	out, err = SubstituteSecrets(onlyFoo, bag, allowed)
	if err != nil || out.Body != "{{foo}}" || out.Headers["X-A"] != "{{notsecret}}" {
		t.Fatalf("non-secret = %+v %v", out, err)
	}
}

func TestSubstituteSecretsRejects(t *testing.T) {
	bag := NewSecretBag()
	bag.Set("access_token", "access-secret-value")
	allowed := map[string]bool{"access_token": true}
	cases := []struct {
		name string
		req  ProbeRequest
		want error
	}{
		{"path", ProbeRequest{Method: "GET", Path: "/v1/{{secret.access_token}}"}, ErrSecretPlaceholder},
		{"host-shaped path", ProbeRequest{Method: "GET", Path: "https://{{secret.access_token}}/x"}, ErrSecretPlaceholder},
		{"method", ProbeRequest{Method: "{{secret.access_token}}", Path: "/v1"}, ErrSecretPlaceholder},
		{"header name", ProbeRequest{Method: "GET", Path: "/v1", Headers: map[string]string{"X-{{secret.access_token}}": "1"}}, ErrSecretPlaceholder},
		{"query key", ProbeRequest{Method: "GET", Path: "/v1", Query: map[string]string{"{{secret.access_token}}": "1"}}, ErrSecretPlaceholder},
		{"body key", ProbeRequest{Method: "POST", Path: "/v1", Body: map[string]any{"{{secret.access_token}}": "1"}}, ErrSecretPlaceholder},
		{"unclosed", ProbeRequest{Method: "GET", Path: "/v1", Headers: map[string]string{"Authorization": "Bearer {{secret.access_token}"}}, ErrSecretPlaceholder},
		{"unclosed prefix", ProbeRequest{Method: "GET", Path: "/v1", Query: map[string]string{"q": "ok {{secret.foo more"}}, ErrSecretPlaceholder},
		{"bad name", ProbeRequest{Method: "GET", Path: "/v1", Headers: map[string]string{"Authorization": "{{secret.Access}}"}}, ErrSecretPlaceholder},
		{"hyphen", ProbeRequest{Method: "GET", Path: "/v1", Headers: map[string]string{"Authorization": "{{secret.access-token}}"}}, ErrSecretPlaceholder},
		{"digit", ProbeRequest{Method: "GET", Path: "/v1", Headers: map[string]string{"Authorization": "{{secret.9abc}}"}}, ErrSecretPlaceholder},
		{"unknown", ProbeRequest{Method: "GET", Path: "/v1", Query: map[string]string{"q": "{{secret.not_a_field}}"}}, ErrSecretFieldUnavailable},
		{"not allowed", ProbeRequest{Method: "GET", Path: "/v1", Query: map[string]string{"q": "{{secret.refresh_token}}"}}, ErrSecretFieldUnavailable},
		{"false allowed", ProbeRequest{Method: "GET", Path: "/v1", Headers: map[string]string{"Authorization": "{{secret.access_token}}"}}, ErrSecretFieldUnavailable},
		{"missing value", ProbeRequest{Method: "GET", Path: "/v1", Query: map[string]string{"q": "{{secret.access_token}}"}}, ErrSecretFieldUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			useBag := bag
			useAllowed := allowed
			switch tc.name {
			case "not allowed":
				useBag = NewSecretBag()
				useBag.Set("refresh_token", "refresh-secret-value")
				useAllowed = map[string]bool{"access_token": true}
			case "false allowed":
				useAllowed = map[string]bool{"access_token": false}
			case "missing value":
				useBag = NewSecretBag()
			}
			out, err := SubstituteSecrets(tc.req, useBag, useAllowed)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v", err)
			}
			if out.Method != "" || out.Path != "" || out.Headers != nil || out.Query != nil || out.Body != nil {
				t.Fatalf("partial request returned: %+v", out)
			}
			if strings.Contains(err.Error(), "access-secret-value") || strings.Contains(err.Error(), "refresh-secret-value") {
				t.Fatalf("error contains a secret: %v", err)
			}
		})
	}
}

func TestSubstituteSecretsDoesNotMutate(t *testing.T) {
	bag := NewSecretBag()
	bag.Set("access_token", "access-secret-value")
	body := map[string]any{"q": "{{secret.access_token}}", "nested": []any{"{{secret.access_token}}"}}
	req := ProbeRequest{
		Method:  "POST",
		Path:    "/v1/chat",
		Headers: map[string]string{"Authorization": "Bearer {{secret.access_token}}"},
		Query:   map[string]string{"token": "{{secret.access_token}}"},
		Body:    body,
	}
	out, err := SubstituteSecrets(req, bag, map[string]bool{"access_token": true})
	if err != nil {
		t.Fatal(err)
	}
	out.Headers["Authorization"] = "changed"
	out.Query["token"] = "changed"
	out.Body.(map[string]any)["q"] = "changed"
	out.Body.(map[string]any)["nested"].([]any)[0] = "changed"
	if req.Headers["Authorization"] != "Bearer {{secret.access_token}}" {
		t.Fatalf("header mutated: %#v", req.Headers)
	}
	if req.Query["token"] != "{{secret.access_token}}" {
		t.Fatalf("query mutated: %#v", req.Query)
	}
	if body["q"] != "{{secret.access_token}}" || body["nested"].([]any)[0] != "{{secret.access_token}}" {
		t.Fatalf("body mutated: %#v", body)
	}
	if req.Path != "/v1/chat" {
		t.Fatal("path")
	}

	original := ProbeRequest{Method: "GET", Path: "/v1/{{secret.access_token}}", Headers: map[string]string{"A": "1"}}
	if _, err := SubstituteSecrets(original, bag, map[string]bool{"access_token": true}); err == nil {
		t.Fatal("expected error")
	}
	if original.Path != "/v1/{{secret.access_token}}" || original.Headers["A"] != "1" {
		t.Fatalf("rejected call mutated input: %+v", original)
	}
}

func TestSubstituteSecretsHeaderInjection(t *testing.T) {
	bag := NewSecretBag()
	bag.Set("access_token", "bad\r\nX-Injected: 1")
	allowed := map[string]bool{"access_token": true}
	_, err := SubstituteSecrets(ProbeRequest{
		Method:  "GET",
		Path:    "/v1",
		Headers: map[string]string{"Authorization": "Bearer {{secret.access_token}}"},
	}, bag, allowed)
	if !errors.Is(err, ErrSecretHeaderInjection) {
		t.Fatalf("header: %v", err)
	}
	if strings.Contains(err.Error(), "X-Injected") || strings.Contains(err.Error(), "bad") {
		t.Fatalf("injection error leaked: %v", err)
	}

	out, err := SubstituteSecrets(ProbeRequest{
		Method: "POST",
		Path:   "/v1",
		Body:   "x {{secret.access_token}}",
		Query:  map[string]string{"q": "{{secret.access_token}}"},
	}, bag, allowed)
	if err != nil {
		t.Fatal(err)
	}
	if out.Body != "x bad\r\nX-Injected: 1" || out.Query["q"] != "bad\r\nX-Injected: 1" {
		t.Fatalf("body/query = %#v %#v", out.Body, out.Query)
	}
	if out.Path != "/v1" {
		t.Fatal("path")
	}

	bag.Set("access_token", "line\nonly")
	_, err = SubstituteSecrets(ProbeRequest{
		Method:  "GET",
		Path:    "/v1",
		Headers: map[string]string{"X-Token": "{{secret.access_token}}"},
	}, bag, allowed)
	if !errors.Is(err, ErrSecretHeaderInjection) {
		t.Fatalf("LF: %v", err)
	}
}

func TestHasSecretPlaceholder(t *testing.T) {
	if HasSecretPlaceholder(ProbeRequest{}) {
		t.Fatal("empty")
	}
	if HasSecretPlaceholder(ProbeRequest{Body: "{{foo}}", Headers: map[string]string{"A": "{{cred.x}}"}}) {
		t.Fatal("non-secret")
	}
	positives := []ProbeRequest{
		{Body: "Bearer {{secret.access_token}}"},
		{Body: map[string]any{"a": []any{map[string]any{"b": "{{secret.access_token}}"}}}},
		{Body: map[string]any{"{{secret.access_token}}": "1"}},
		{Query: map[string]string{"q": "{{secret.access_token}}"}},
		{Query: map[string]string{"{{secret.access_token}}": "1"}},
		{Headers: map[string]string{"Authorization": "{{secret.access_token}}"}},
		{Headers: map[string]string{"X-{{secret.access_token}}": "1"}},
		{Path: "/{{secret.access_token}}"},
		{Method: "{{secret.access_token}}"},
		{Body: "ok {{secret.foo"},
	}
	for i, req := range positives {
		if !HasSecretPlaceholder(req) {
			t.Fatalf("case %d should match: %+v", i, req)
		}
	}
	if HasSecretPlaceholder(ProbeRequest{Body: "{{ secret.access_token }}"}) {
		t.Fatal("spaced form is not a secret placeholder")
	}
}

func TestRedactResponseShortSecretStaysElsewhere(t *testing.T) {
	// Under four code points the value is still lifted out of the declared
	// path, but Scrub will not hunt it down elsewhere.
	body := []byte(`{"token":"abc","echo":"abc"}`)
	bag := NewSecretBag()
	out, js, err := RedactResponse("application/json", body, map[string]string{"token": "access_token"}, bag)
	if err != nil {
		t.Fatal(err)
	}
	if mustStringAt(t, js, "token") != SecretRedactedPlaceholder {
		t.Fatalf("declared = %#v", js)
	}
	if mustStringAt(t, js, "echo") != "abc" {
		t.Fatalf("echo = %#v", js)
	}
	if v, _ := bag.Get("access_token"); v != "abc" {
		t.Fatalf("bag = %q", v)
	}
	if !bytes.Contains(out, []byte("abc")) {
		t.Fatalf("short copy was scrubbed: %s", out)
	}
}

func TestRedactResponseNestedIndexes(t *testing.T) {
	body := []byte(`{"a":[["nope","the-secret-value-here"]]}`)
	bag := NewSecretBag()
	out, js, err := RedactResponse("application/json", body, map[string]string{"a[0][1]": "access_token"}, bag)
	if err != nil {
		t.Fatal(err)
	}
	assertConsistent(t, out, js)
	assertNoSecret(t, out, js, "the-secret-value-here")
	if mustStringAt(t, js, "a[0][0]") != "nope" || mustStringAt(t, js, "a[0][1]") != SecretRedactedPlaceholder {
		t.Fatalf("js = %#v", js)
	}
}

func randomRedactCase(rng *rand.Rand) (body []byte, rules map[string]string, secrets []string) {
	rules = map[string]string{}
	root := map[string]any{"id": json.Number("9007199254740993")}
	count := 1 + rng.Intn(3)
	for i := 0; i < count; i++ {
		sec := randomSecret(rng, i)
		secrets = append(secrets, sec)
		field := fmt.Sprintf("field_%d", i)
		switch rng.Intn(3) {
		case 0:
			key := fmt.Sprintf("f%d", i)
			root[key] = sec
			rules[key] = field
		case 1:
			key := fmt.Sprintf("o%d", i)
			root[key] = map[string]any{"token": sec, "echo": "copy " + sec}
			rules[key+".token"] = field
		default:
			key := fmt.Sprintf("a%d", i)
			root[key] = []any{map[string]any{"token": sec}, sec}
			rules[fmt.Sprintf("%s[0].token", key)] = field
		}
	}
	root["copy"] = secrets[0]
	root["k_"+secrets[0]] = 1
	root["arr"] = []any{[]any{secrets[0], "ok"}}
	rules["no.such.value"] = "not_here"
	encoded, err := json.Marshal(root)
	if err != nil {
		panic(err)
	}
	return encoded, rules, secrets
}

func randomSecret(rng *rand.Rand, n int) string {
	const alpha = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	buf := make([]byte, 12)
	for i := range buf {
		buf[i] = alpha[rng.Intn(len(alpha))]
	}
	s := fmt.Sprintf("S%d%s", n, buf)
	switch n % 5 {
	case 1:
		s += `"`
	case 2:
		s += `\`
	case 3:
		s += "😀"
	case 4:
		s += "</script>"
	}
	return s
}

func utf8RuneCount(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

func mustPath(t *testing.T, raw string) SecretPath {
	t.Helper()
	path, err := ParseSecretPath(raw)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func mustStringAt(t *testing.T, v any, raw string) string {
	t.Helper()
	got, ok := walk(v, mustPath(t, raw), nil, false)
	if !ok {
		t.Fatalf("missing %s in %#v", raw, v)
	}
	text, ok := got.(string)
	if !ok {
		t.Fatalf("%s is %T (%#v)", raw, got, got)
	}
	return text
}

func assertConsistent(t *testing.T, body []byte, js any) {
	t.Helper()
	again, err := json.Marshal(js)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(again, body) {
		t.Fatalf("body and JSON diverge\nbody %s\njson %s", body, again)
	}
}

func assertNoSecret(t *testing.T, body []byte, js any, secrets ...string) {
	t.Helper()
	encoded, err := json.Marshal(js)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		frag := jsonStringFragment(secret)
		if bytes.Contains(body, []byte(secret)) || (frag != secret && bytes.Contains(body, []byte(frag))) {
			t.Fatalf("body contains %q\n%s", secret, body)
		}
		if bytes.Contains(encoded, []byte(secret)) || (frag != secret && bytes.Contains(encoded, []byte(frag))) {
			t.Fatalf("JSON encoding contains %q\n%s", secret, encoded)
		}
		if treeContains(js, secret) || (frag != secret && treeContains(js, frag)) {
			t.Fatalf("JSON value contains %q\n%s", secret, encoded)
		}
	}
}

func treeContains(v any, needle string) bool {
	switch typed := v.(type) {
	case string:
		return strings.Contains(typed, needle)
	case map[string]any:
		for key, val := range typed {
			if strings.Contains(key, needle) || treeContains(val, needle) {
				return true
			}
		}
	case []any:
		for _, val := range typed {
			if treeContains(val, needle) {
				return true
			}
		}
	}
	return false
}
