package jsplugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	// SecretRedactedPlaceholder replaces a secret at a declared path.
	// Both the response body and the parsed JSON must use this exact text.
	SecretRedactedPlaceholder = "{{redacted}}"

	// secretScrubMark replaces a secret that shows up outside a declared path.
	// It is not a secret placeholder and must not be substituted later.
	secretScrubMark = "[redacted]"

	// minScrubRunes is the shortest secret Scrub will erase. Shorter values
	// collide with ordinary words. Length is Unicode code points, not bytes,
	// so a single emoji is not treated as a four-byte token.
	minScrubRunes = 4

	secretPrefix = "{{secret."
)

var (
	// ErrInvalidSecretPath is a secretPaths syntax error.
	ErrInvalidSecretPath = errors.New("invalid secret path")
	// ErrSecretRedactNotJSON means rules were set but the response is not JSON.
	// Callers must drop the original body; it is not returned.
	ErrSecretRedactNotJSON = errors.New("response is not JSON")
	// ErrSecretRedactNotObject means the JSON root is an array or a scalar.
	ErrSecretRedactNotObject = errors.New("JSON root is not an object")
	// ErrSecretValueNotString means a declared path exists but is not a string.
	ErrSecretValueNotString = errors.New("secret path value is not a string")
	// ErrSecretRedactLeak means a secret would still be visible after redaction.
	// The original body is not returned.
	ErrSecretRedactLeak = errors.New("secret survived redaction")
	// ErrNilSecretBag means rules were set without a bag to hold the secrets.
	ErrNilSecretBag = errors.New("secret bag is nil")
	// ErrSecretFieldName means a rule's field name cannot be used as {{secret.<field>}}.
	ErrSecretFieldName = errors.New("invalid secret field name")
	// ErrSecretFieldUnavailable means the placeholder names a field that is
	// not allowed or has no value. The error names the field and never the value.
	ErrSecretFieldUnavailable = errors.New("secret field is not available")
	// ErrSecretPlaceholder means a {{secret.*}} attempt is malformed or sits
	// somewhere substitution is forbidden.
	ErrSecretPlaceholder = errors.New("invalid secret placeholder")
	// ErrSecretHeaderInjection means a secret inserted into a header contains CR or LF.
	ErrSecretHeaderInjection = errors.New("header value contains CR or LF")
)

// SecretPath is one secretPaths entry: object keys and non-negative indexes.
// Wildcards are not supported.
type SecretPath struct {
	segs []secretSeg
}

type secretSeg struct {
	key     string
	index   int
	isIndex bool
}

// String returns the canonical path. A leading-zero index is normalized.
func (p SecretPath) String() string {
	var b strings.Builder
	for _, seg := range p.segs {
		if seg.isIndex {
			fmt.Fprintf(&b, "[%d]", seg.index)
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('.')
		}
		b.WriteString(seg.key)
	}
	return b.String()
}

// ParseSecretPath parses dot keys and [index] suffixes.
// Keys are [A-Za-z0-9_-]. Indexes are non-negative integers.
// Empty segments, "..", a leading or trailing dot, an unclosed bracket,
// and a negative index are errors.
func ParseSecretPath(raw string) (SecretPath, error) {
	if raw == "" {
		return SecretPath{}, fmt.Errorf("%w: empty", ErrInvalidSecretPath)
	}
	var segs []secretSeg
	for i := 0; i < len(raw); {
		if !isSecretKeyChar(raw[i]) {
			return SecretPath{}, fmt.Errorf("%w: %q", ErrInvalidSecretPath, raw)
		}
		start := i
		for i < len(raw) && isSecretKeyChar(raw[i]) {
			i++
		}
		segs = append(segs, secretSeg{key: raw[start:i]})
		for i < len(raw) && raw[i] == '[' {
			i++
			if i < len(raw) && raw[i] == '-' {
				return SecretPath{}, fmt.Errorf("%w: negative index", ErrInvalidSecretPath)
			}
			numStart := i
			for i < len(raw) && raw[i] >= '0' && raw[i] <= '9' {
				i++
			}
			if numStart == i || i >= len(raw) || raw[i] != ']' {
				return SecretPath{}, fmt.Errorf("%w: bad index", ErrInvalidSecretPath)
			}
			n, err := strconv.Atoi(raw[numStart:i])
			if err != nil || n < 0 {
				return SecretPath{}, fmt.Errorf("%w: bad index", ErrInvalidSecretPath)
			}
			i++
			segs = append(segs, secretSeg{index: n, isIndex: true})
		}
		if i == len(raw) {
			break
		}
		if raw[i] != '.' {
			return SecretPath{}, fmt.Errorf("%w: %q", ErrInvalidSecretPath, raw)
		}
		i++
		if i == len(raw) || raw[i] == '.' || raw[i] == '[' {
			return SecretPath{}, fmt.Errorf("%w: empty segment", ErrInvalidSecretPath)
		}
	}
	if len(segs) == 0 {
		return SecretPath{}, fmt.Errorf("%w: empty", ErrInvalidSecretPath)
	}
	return SecretPath{segs: segs}, nil
}

func isSecretKeyChar(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}

// secretFieldOK matches placeholder names: [a-z][a-z0-9_]*.
func secretFieldOK(name string) bool {
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// SecretBag holds field → secret value in host memory only.
//
// A bag is not safe for concurrent use. It belongs to one call chain and
// must not be shared between goroutines. It has no lock on purpose: a lock
// would hide accidental sharing of secrets across requests.
type SecretBag struct {
	values map[string]string
	// extras are secrets that must be scrubbed but are not the current
	// value of any field (a rotated value, or a later path that lost).
	extras []string
}

// NewSecretBag returns an empty bag.
func NewSecretBag() *SecretBag {
	return &SecretBag{values: map[string]string{}}
}

// Set stores value under field, replacing the previous value.
// The previous value stays in the scrub set so a rotated token cannot
// leak through a later log line. The caller must not log value.
func (b *SecretBag) Set(field, value string) {
	if b.values == nil {
		b.values = map[string]string{}
	}
	if old, ok := b.values[field]; ok && old != value {
		b.values[field] = value
		b.note(old)
		return
	}
	b.values[field] = value
}

// Get returns the current value of field.
func (b *SecretBag) Get(field string) (string, bool) {
	if b == nil {
		return "", false
	}
	v, ok := b.values[field]
	return v, ok
}

// Has reports whether field has a stored value.
func (b *SecretBag) Has(field string) bool {
	if b == nil {
		return false
	}
	_, ok := b.values[field]
	return ok
}

// Fields returns the stored field names in lexicographic order.
func (b *SecretBag) Fields() []string {
	if b == nil || len(b.values) == 0 {
		return nil
	}
	out := make([]string, 0, len(b.values))
	for field := range b.values {
		out = append(out, field)
	}
	sort.Strings(out)
	return out
}

// note remembers value for scrubbing when it is not already stored.
func (b *SecretBag) note(value string) {
	if value == "" {
		return
	}
	for _, existing := range b.values {
		if existing == value {
			return
		}
	}
	for _, existing := range b.extras {
		if existing == value {
			return
		}
	}
	b.extras = append(b.extras, value)
}

func (b *SecretBag) eachValue(fn func(string)) {
	if b == nil {
		return
	}
	for _, value := range b.values {
		fn(value)
	}
	for _, value := range b.extras {
		fn(value)
	}
}

// needles are the strings Scrub erases, longest first.
// A value under four code points is ignored. Its JSON encoding is included
// so a secret that contains quotes, backslashes, or non-ASCII is erased
// both raw and as it appears inside a JSON string.
func (b *SecretBag) needles() []string {
	if b == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	add := func(v string) {
		if v == "" || utf8.RuneCountInString(v) < minScrubRunes {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	b.eachValue(func(v string) {
		if utf8.RuneCountInString(v) < minScrubRunes {
			return
		}
		add(v)
		frag := jsonStringFragment(v)
		if frag != v {
			add(frag)
		}
	})
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) > len(out[j])
		}
		return out[i] < out[j]
	})
	return out
}

// Scrub replaces every scrubbable secret in s with [redacted].
// Longer values are replaced before shorter ones so a short secret cannot
// split a longer one. A value that is itself inside [redacted] cannot be
// removed by substitution; RedactResponse then fails closed.
func (b *SecretBag) Scrub(s string) string {
	if b == nil || s == "" {
		return s
	}
	for _, needle := range b.needles() {
		s = replaceNeedle(s, needle, secretScrubMark)
	}
	return s
}

func replaceNeedle(s, needle, mark string) string {
	if needle == "" || strings.Contains(mark, needle) {
		return s
	}
	for i := 0; i < 8 && strings.Contains(s, needle); i++ {
		next := strings.ReplaceAll(s, needle, mark)
		if next == s {
			break
		}
		s = next
	}
	return s
}

// ScrubJSON returns a copy of a decoded JSON value with secrets removed from
// string values and object keys. Keys are not addressable by secretPaths, so
// a secret that appears in a key is erased here. Two keys that scrub to the
// same string collapse; the lexicographically later original key wins, and
// the secret does not remain in either key. The input is not modified.
// The bag is not safe for concurrent calls.
func (b *SecretBag) ScrubJSON(v any) any {
	if b == nil {
		return v
	}
	switch typed := v.(type) {
	case string:
		return b.Scrub(typed)
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(typed))
		for _, key := range keys {
			out[b.Scrub(key)] = b.ScrubJSON(typed[key])
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, val := range typed {
			out[i] = b.ScrubJSON(val)
		}
		return out
	default:
		return v
	}
}

// scrubbedError exposes scrubbed text without changing unwrap semantics.
type scrubbedError struct {
	err error
	msg string
}

func (e *scrubbedError) Error() string { return e.msg }

func (e *scrubbedError) Unwrap() error { return e.err }

// ScrubError returns an error whose Error text has secrets removed.
// errors.Is and errors.As still see the wrapped error. Structured fields on
// that inner error are not rewritten; do not log them.
func (b *SecretBag) ScrubError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if b != nil {
		msg = b.Scrub(msg)
	}
	if msg == err.Error() {
		return err
	}
	return &scrubbedError{err: err, msg: msg}
}

// RedactResponse pulls secrets named by rules out of a JSON object and into bag.
// rules maps a secret path to a credential field name ([a-z][a-z0-9_]*).
//
// An empty rule set returns body unchanged and the json.Unmarshal result, or
// nil when the body is not JSON. That matches the probe hook today.
//
// A non-empty rule set requires a JSON object. A non-JSON response, including
// an HTML error page, returns ErrSecretRedactNotJSON and a nil body — never
// the original bytes. A path that is missing is not an error and does not
// write the bag. A path that exists but is not a non-empty string is an error.
// When several paths share a field name, paths are applied in lexicographic
// order and the first non-empty string is the one Get returns; every hit is
// still removed from the document.
//
// The returned body and JSON value match and do not contain scrubbable
// secrets. On any error the body is nil and must not be shown to JS.
// Object key order in the body is not preserved. Numbers are decoded with
// UseNumber so a large integer survives the re-encode.
func RedactResponse(contentType string, body []byte, rules map[string]string, bag *SecretBag) ([]byte, any, error) {
	if len(rules) == 0 {
		var payload any
		if json.Unmarshal(body, &payload) != nil {
			payload = nil
		}
		return body, payload, nil
	}
	if bag == nil {
		return nil, nil, ErrNilSecretBag
	}
	if !contentTypeAllowsJSON(contentType) {
		return nil, nil, ErrSecretRedactNotJSON
	}
	root, err := decodeJSONUseNumber(body)
	if err != nil {
		return nil, nil, ErrSecretRedactNotJSON
	}
	obj, ok := root.(map[string]any)
	if !ok {
		return nil, nil, ErrSecretRedactNotObject
	}

	paths := make([]string, 0, len(rules))
	for path := range rules {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	type hit struct {
		path  SecretPath
		field string
		value string
	}
	hits := make([]hit, 0, len(paths))
	for _, rawPath := range paths {
		field := rules[rawPath]
		if !secretFieldOK(field) {
			return nil, nil, fmt.Errorf("%w: %q", ErrSecretFieldName, field)
		}
		parsed, err := ParseSecretPath(rawPath)
		if err != nil {
			return nil, nil, fmt.Errorf("secret path %q: %w", rawPath, err)
		}
		val, found := walk(obj, parsed, nil, false)
		if !found {
			continue
		}
		text, isString := val.(string)
		if !isString {
			return nil, nil, fmt.Errorf("%w: %s", ErrSecretValueNotString, rawPath)
		}
		if text == "" {
			continue
		}
		hits = append(hits, hit{path: parsed, field: field, value: text})
	}
	for _, item := range hits {
		if !bag.Has(item.field) {
			bag.Set(item.field, item.value)
		}
		bag.note(item.value)
	}

	sentinel := ""
	if len(hits) > 0 {
		sentinel, err = pickSentinel(bag, obj)
		if err != nil {
			return nil, nil, ErrSecretRedactLeak
		}
		for _, item := range hits {
			if _, ok := walk(obj, item.path, sentinel, true); !ok {
				return nil, nil, ErrSecretRedactLeak
			}
		}
	}
	return finishRedact(bag, obj, sentinel)
}

func contentTypeAllowsJSON(contentType string) bool {
	contentType = strings.TrimSpace(contentType)
	if contentType == "" {
		return true
	}
	media, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	return media == "application/json" || media == "text/json" || strings.HasSuffix(media, "+json")
}

func decodeJSONUseNumber(body []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return nil, errors.New("trailing JSON value")
	}
	return v, nil
}

func walk(root any, path SecretPath, replace any, doReplace bool) (any, bool) {
	if len(path.segs) == 0 {
		return nil, false
	}
	cur := root
	last := len(path.segs) - 1
	for i, seg := range path.segs {
		if seg.isIndex {
			arr, ok := cur.([]any)
			if !ok || seg.index < 0 || seg.index >= len(arr) {
				return nil, false
			}
			if doReplace && i == last {
				arr[seg.index] = replace
				return replace, true
			}
			cur = arr[seg.index]
			continue
		}
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		val, exists := obj[seg.key]
		if !exists {
			return nil, false
		}
		if doReplace && i == last {
			obj[seg.key] = replace
			return replace, true
		}
		cur = val
	}
	return cur, true
}

func pickSentinel(bag *SecretBag, root any) (string, error) {
	blob, err := json.Marshal(root)
	if err != nil {
		return "", err
	}
	text := string(blob)
	for i := 0; i < 64; i++ {
		candidate := fmt.Sprintf("\uE000S%d\uE000", i)
		if strings.Contains(text, candidate) || bag.conflicts(candidate) {
			continue
		}
		return candidate, nil
	}
	return "", errors.New("no sentinel")
}

func (b *SecretBag) conflicts(sentinel string) bool {
	hit := false
	b.eachValue(func(v string) {
		if hit || v == "" {
			return
		}
		if strings.Contains(v, sentinel) || (utf8.RuneCountInString(v) >= minScrubRunes && strings.Contains(sentinel, v)) {
			hit = true
			return
		}
		frag := jsonStringFragment(v)
		if frag == v {
			return
		}
		if strings.Contains(frag, sentinel) || (utf8.RuneCountInString(frag) >= minScrubRunes && strings.Contains(sentinel, frag)) {
			hit = true
		}
	})
	return hit
}

// finishRedact scrubs every bag secret from strings and keys, then again from
// the re-encoded body so a JSON-escaped copy cannot survive. Declared paths
// hold sentinel until after that scrub, then become {{redacted}}.
func finishRedact(bag *SecretBag, root any, sentinel string) ([]byte, any, error) {
	encoded, err := json.Marshal(bag.ScrubJSON(root))
	if err != nil {
		return nil, nil, ErrSecretRedactLeak
	}
	text := bag.Scrub(string(encoded))
	if sentinel != "" {
		text = strings.ReplaceAll(text, sentinel, SecretRedactedPlaceholder)
		if frag := jsonStringFragment(sentinel); frag != sentinel {
			text = strings.ReplaceAll(text, frag, SecretRedactedPlaceholder)
		}
		if strings.Contains(text, sentinel) {
			return nil, nil, ErrSecretRedactLeak
		}
	}
	if bag.containsNeedle(text) {
		return nil, nil, ErrSecretRedactLeak
	}
	parsed, err := decodeJSONUseNumber([]byte(text))
	if err != nil || bag.treeHasNeedle(parsed) {
		return nil, nil, ErrSecretRedactLeak
	}
	canonical, err := json.Marshal(parsed)
	if err != nil || bag.containsNeedle(string(canonical)) || (sentinel != "" && strings.Contains(string(canonical), sentinel)) {
		return nil, nil, ErrSecretRedactLeak
	}
	return canonical, parsed, nil
}

func (b *SecretBag) containsNeedle(s string) bool {
	for _, needle := range b.needles() {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

func (b *SecretBag) treeHasNeedle(v any) bool {
	switch typed := v.(type) {
	case string:
		return b.containsNeedle(typed)
	case map[string]any:
		for key, val := range typed {
			if b.containsNeedle(key) || b.treeHasNeedle(val) {
				return true
			}
		}
	case []any:
		for _, val := range typed {
			if b.treeHasNeedle(val) {
				return true
			}
		}
	}
	return false
}

func jsonStringFragment(v string) string {
	encoded, err := json.Marshal(v)
	if err != nil || len(encoded) < 2 {
		return v
	}
	return string(encoded[1 : len(encoded)-1])
}

// HasSecretPlaceholder reports whether req contains a {{secret. attempt,
// including a malformed or unclosed one, in the path, method, header name or
// value, query key or value, or any body string or object key.
// A true result means the caller must run SubstituteSecrets and must not
// forward the template unchanged.
func HasSecretPlaceholder(req ProbeRequest) bool {
	if strings.Contains(req.Path, secretPrefix) || strings.Contains(req.Method, secretPrefix) {
		return true
	}
	for key, val := range req.Headers {
		if strings.Contains(key, secretPrefix) || strings.Contains(val, secretPrefix) {
			return true
		}
	}
	for key, val := range req.Query {
		if strings.Contains(key, secretPrefix) || strings.Contains(val, secretPrefix) {
			return true
		}
	}
	return valueHasSecretPlaceholder(req.Body)
}

func valueHasSecretPlaceholder(v any) bool {
	switch typed := v.(type) {
	case string:
		return strings.Contains(typed, secretPrefix)
	case map[string]any:
		for key, val := range typed {
			if strings.Contains(key, secretPrefix) || valueHasSecretPlaceholder(val) {
				return true
			}
		}
	case []any:
		for _, val := range typed {
			if valueHasSecretPlaceholder(val) {
				return true
			}
		}
	}
	return false
}

// SubstituteSecrets replaces {{secret.<field>}} in body strings, query values,
// and header values. field must match [a-z][a-z0-9_]*, be set in allowed, and
// have a value in bag. Other {{...}} forms are left unchanged.
//
// A placeholder in the path, method, header name, query key, or body object
// key is an error and is not replaced. Path is not parsed as a URL and is
// returned unchanged, so this function cannot move the host that the caller
// later joins. The input request is not modified.
//
// A secret that contains CR or LF is rejected when it would be written into
// a header value. Error text names the field and never includes a secret.
func SubstituteSecrets(req ProbeRequest, bag *SecretBag, allowed map[string]bool) (ProbeRequest, error) {
	if err := rejectSecretLocations(req); err != nil {
		return ProbeRequest{}, err
	}
	headers, err := substituteStringMap(req.Headers, bag, allowed, true)
	if err != nil {
		return ProbeRequest{}, err
	}
	query, err := substituteStringMap(req.Query, bag, allowed, false)
	if err != nil {
		return ProbeRequest{}, err
	}
	body, err := substituteValue(req.Body, bag, allowed)
	if err != nil {
		return ProbeRequest{}, err
	}
	return ProbeRequest{
		Method:  req.Method,
		Path:    req.Path,
		Headers: headers,
		Query:   query,
		Body:    body,
	}, nil
}

func rejectSecretLocations(req ProbeRequest) error {
	if strings.Contains(req.Path, secretPrefix) {
		return fmt.Errorf("%w in path", ErrSecretPlaceholder)
	}
	if strings.Contains(req.Method, secretPrefix) {
		return fmt.Errorf("%w in method", ErrSecretPlaceholder)
	}
	for key := range req.Headers {
		if strings.Contains(key, secretPrefix) {
			return fmt.Errorf("%w in header name", ErrSecretPlaceholder)
		}
	}
	for key := range req.Query {
		if strings.Contains(key, secretPrefix) {
			return fmt.Errorf("%w in query key", ErrSecretPlaceholder)
		}
	}
	return nil
}

func substituteStringMap(in map[string]string, bag *SecretBag, allowed map[string]bool, inHeader bool) (map[string]string, error) {
	if in == nil {
		return nil, nil
	}
	out := make(map[string]string, len(in))
	for key, val := range in {
		next, err := substituteString(val, bag, allowed, inHeader)
		if err != nil {
			return nil, err
		}
		out[key] = next
	}
	return out, nil
}

func substituteValue(v any, bag *SecretBag, allowed map[string]bool) (any, error) {
	switch typed := v.(type) {
	case nil:
		return nil, nil
	case string:
		return substituteString(typed, bag, allowed, false)
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, val := range typed {
			if strings.Contains(key, secretPrefix) {
				return nil, fmt.Errorf("%w in body key", ErrSecretPlaceholder)
			}
			next, err := substituteValue(val, bag, allowed)
			if err != nil {
				return nil, err
			}
			out[key] = next
		}
		return out, nil
	case []any:
		out := make([]any, len(typed))
		for i, val := range typed {
			next, err := substituteValue(val, bag, allowed)
			if err != nil {
				return nil, err
			}
			out[i] = next
		}
		return out, nil
	default:
		return v, nil
	}
}

func substituteString(s string, bag *SecretBag, allowed map[string]bool, inHeader bool) (string, error) {
	var b strings.Builder
	rest := s
	for {
		start := strings.Index(rest, "{{")
		if start < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:start])
		if strings.HasPrefix(rest[start:], secretPrefix) {
			field, n, err := takeSecretField(rest[start:])
			if err != nil {
				return "", err
			}
			if !allowed[field] || bag == nil || !bag.Has(field) {
				return "", fmt.Errorf("%w: %s", ErrSecretFieldUnavailable, field)
			}
			value, _ := bag.Get(field)
			if inHeader && strings.ContainsAny(value, "\r\n") {
				return "", ErrSecretHeaderInjection
			}
			b.WriteString(value)
			rest = rest[start+n:]
			continue
		}
		end := strings.Index(rest[start+2:], "}}")
		if end < 0 {
			b.WriteString(rest[start:])
			break
		}
		end = start + 2 + end + 2
		b.WriteString(rest[start:end])
		rest = rest[end:]
	}
	return b.String(), nil
}

// takeSecretField parses a placeholder at the start of s, which begins with {{secret.
func takeSecretField(s string) (field string, n int, err error) {
	rest := s[len(secretPrefix):]
	if rest == "" || rest[0] < 'a' || rest[0] > 'z' {
		return "", 0, fmt.Errorf("%w: unclosed or invalid", ErrSecretPlaceholder)
	}
	i := 1
	for i < len(rest) {
		c := rest[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
			i++
			continue
		}
		break
	}
	if i+1 >= len(rest) || rest[i] != '}' || rest[i+1] != '}' {
		return "", 0, fmt.Errorf("%w: unclosed or invalid", ErrSecretPlaceholder)
	}
	return rest[:i], len(secretPrefix) + i + 2, nil
}
