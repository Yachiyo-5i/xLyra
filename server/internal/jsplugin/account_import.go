package jsplugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// maxAccountImportBytes is the largest file ParseAccountImport accepts.
	// Callers can refuse a smaller upload before calling.
	maxAccountImportBytes = 4 << 20
	// maxAccountImportRecords is the most account objects one file may contain.
	maxAccountImportRecords = 500
	// unixMillisThreshold is the smallest numeric expiry read as Unix milliseconds.
	// Positive numbers below it are Unix seconds. 1e12 ms is 2001-09-09, so
	// current Unix seconds stay on the seconds side of the cut.
	unixMillisThreshold int64 = 1_000_000_000_000
)

var (
	errAccountImportConfig     = errors.New("account import config is required")
	errAccountImportTooLarge   = errors.New("account import file exceeds 4 MiB")
	errAccountImportEmpty      = errors.New("account import file is empty")
	errAccountImportJSON       = errors.New("account import file is not valid JSON")
	errAccountImportShape      = errors.New("account import file must contain an account object or an array of accounts")
	errAccountImportNoAccounts = errors.New("account import file contains no accounts")
	errAccountImportTooMany    = errors.New("account import file contains more than 500 accounts")
)

// ImportedAccount is one account taken from an uploaded JSON file.
// Fields are credential-bag values keyed by declared field name. When the
// expires field is present it is stored as canonical Unix seconds, and
// ExpiresAt is that instant (sub-second precision included). A zero ExpiresAt
// means the file omitted the expires field.
type ImportedAccount struct {
	Fields    map[string]string
	ExpiresAt time.Time
}

type accountImportField struct {
	secret bool
	typ    string
}

// ParseAccountImport maps raw, an uploaded JSON account file, onto credential
// fields described by cfg and credFields.
//
// raw may be one JSON object, an array of objects, or an object whose value at
// the first matching cfg.Containers path is an object or an array. Container
// paths are dot-separated (data.accounts). FieldMap aliases are top-level keys
// of each account object, not paths, and are tried in order; the first
// non-empty string or number wins. Other JSON types are skipped.
//
// A numeric expires value at or above the millisecond threshold is Unix
// milliseconds. The threshold is cfg.MsThreshold, or unixMillisThreshold when
// that pointer is nil. Smaller positive numbers are Unix seconds. Numeric
// strings and RFC3339 strings are accepted as well. valueMap rewrites a mapped
// field after the alias is chosen, by exact match. It does not rewrite the
// expires field. When the expires field is still empty and credential.expiresFrom
// is claims.exp, the access token's exp claim fills it.
//
// Mapped secret fields are required: if one is missing, or a present expires
// value cannot be parsed, the whole file is rejected. The error names the
// 1-based account index and the field name, and does not include any field
// value. Duplicate accounts (same field values and the same expiry instant)
// are dropped, keeping the first.
func ParseAccountImport(raw []byte, cfg *SiteProviderImport, cred SiteProviderCredential) ([]ImportedAccount, error) {
	if cfg == nil {
		return nil, errAccountImportConfig
	}
	if len(raw) > maxAccountImportBytes {
		return nil, errAccountImportTooLarge
	}
	declared := declaredImportFields(cred.Fields)
	if err := rejectUnknownImportFields(cfg.FieldMap, declared); err != nil {
		return nil, err
	}
	if err := rejectBadExpiresField(cred.ExpiresAtField, declared); err != nil {
		return nil, err
	}
	root, err := decodeAccountImport(raw)
	if err != nil {
		return nil, err
	}
	objects, err := resolveAccountObjects(root, cfg.Containers)
	if err != nil {
		return nil, err
	}
	accounts := make([]ImportedAccount, 0, len(objects))
	threshold := importMillisThreshold(cfg)
	for i, object := range objects {
		account, mapErr := mapImportedAccount(object, cfg, declared, cred, threshold, i+1)
		if mapErr != nil {
			return nil, mapErr
		}
		accounts = append(accounts, account)
	}
	return dedupImportedAccounts(accounts), nil
}

func importMillisThreshold(cfg *SiteProviderImport) int64 {
	if cfg != nil && cfg.MsThreshold != nil {
		return *cfg.MsThreshold
	}
	return unixMillisThreshold
}

func declaredImportFields(fields []SiteProviderField) map[string]accountImportField {
	declared := make(map[string]accountImportField, len(fields))
	for _, field := range fields {
		if _, exists := declared[field.Name]; exists {
			continue
		}
		declared[field.Name] = accountImportField{secret: field.Secret, typ: field.Type}
	}
	return declared
}

func rejectUnknownImportFields(fieldMap map[string][]string, declared map[string]accountImportField) error {
	unknown := make([]string, 0)
	for name := range fieldMap {
		if _, ok := declared[name]; !ok {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fmt.Errorf("import fieldMap %q is not a declared credential field", unknown[0])
}

func rejectBadExpiresField(name string, declared map[string]accountImportField) error {
	if name == "" {
		return nil
	}
	meta, ok := declared[name]
	if !ok {
		return fmt.Errorf("import expires field %q is not a declared credential field", name)
	}
	if meta.typ != "unix" {
		return fmt.Errorf("import expires field %q must have type %q", name, "unix")
	}
	return nil
}

func decodeAccountImport(raw []byte) (any, error) {
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, errAccountImportEmpty
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var root any
	if err := dec.Decode(&root); err != nil {
		return nil, errAccountImportJSON
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, errAccountImportJSON
	}
	return root, nil
}

// resolveAccountObjects picks the account list out of the three accepted shapes.
// The first container path that resolves to an object or array wins, including
// an empty array. A path that is missing or lands on a scalar is skipped.
func resolveAccountObjects(root any, containers []string) ([]map[string]any, error) {
	switch value := root.(type) {
	case []any:
		return accountObjectsFromArray(value)
	case map[string]any:
		for _, container := range containers {
			found, ok := lookupImportContainer(value, container)
			if !ok {
				continue
			}
			switch wrapped := found.(type) {
			case []any:
				return accountObjectsFromArray(wrapped)
			case map[string]any:
				return []map[string]any{wrapped}, nil
			}
		}
		return []map[string]any{value}, nil
	default:
		return nil, errAccountImportShape
	}
}

func lookupImportContainer(root map[string]any, path string) (any, bool) {
	if path == "" {
		return nil, false
	}
	current := any(root)
	for _, part := range strings.Split(path, ".") {
		if part == "" {
			return nil, false
		}
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		next, exists := object[part]
		if !exists {
			return nil, false
		}
		current = next
	}
	switch current.(type) {
	case []any, map[string]any:
		return current, true
	default:
		return nil, false
	}
}

func accountObjectsFromArray(items []any) ([]map[string]any, error) {
	if len(items) == 0 {
		return nil, errAccountImportNoAccounts
	}
	if len(items) > maxAccountImportRecords {
		return nil, errAccountImportTooMany
	}
	objects := make([]map[string]any, len(items))
	for i, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("account %d must be a JSON object", i+1)
		}
		objects[i] = object
	}
	return objects, nil
}

func mapImportedAccount(object map[string]any, cfg *SiteProviderImport, declared map[string]accountImportField, cred SiteProviderCredential, threshold int64, index int) (ImportedAccount, error) {
	names := make([]string, 0, len(cfg.FieldMap))
	for name := range cfg.FieldMap {
		names = append(names, name)
	}
	sort.Strings(names)
	fields := make(map[string]string, len(names))
	var expiresAt time.Time
	for _, name := range names {
		text, ok := firstImportAlias(object, cfg.FieldMap[name])
		if name == cred.ExpiresAtField {
			if !ok {
				if declared[name].secret {
					return ImportedAccount{}, fmt.Errorf("account %d is missing required field %q", index, name)
				}
				continue
			}
			parsed, good := parseImportedExpiry(text, threshold)
			if !good {
				return ImportedAccount{}, fmt.Errorf("account %d has an invalid %q", index, name)
			}
			expiresAt = parsed
			fields[name] = strconv.FormatInt(parsed.Unix(), 10)
			continue
		}
		if !ok {
			if declared[name].secret {
				return ImportedAccount{}, fmt.Errorf("account %d is missing required field %q", index, name)
			}
			continue
		}
		fields[name] = applyImportValueMap(text, cfg.ValueMap[name])
	}
	spec := AccountPoolSpec{Credential: cred}
	FillExpiresFrom(spec, fields)
	if name := cred.ExpiresAtField; name != "" && fields[name] != "" && expiresAt.IsZero() {
		if unix, err := strconv.ParseInt(fields[name], 10, 64); err == nil && unix > 0 {
			expiresAt = time.Unix(unix, 0).UTC()
		}
	}
	return ImportedAccount{Fields: fields, ExpiresAt: expiresAt}, nil
}

func applyImportValueMap(text string, entries map[string]string) string {
	if len(entries) == 0 {
		return text
	}
	if mapped, ok := entries[text]; ok {
		return mapped
	}
	for source, mapped := range entries {
		if strings.EqualFold(source, text) {
			return mapped
		}
	}
	return text
}

func firstImportAlias(object map[string]any, aliases []string) (string, bool) {
	for _, alias := range aliases {
		if alias == "" {
			continue
		}
		text, ok := importAliasValue(object[alias])
		if ok {
			return text, true
		}
	}
	return "", false
}

func importAliasValue(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return "", false
		}
		return trimmed, true
	case json.Number:
		text := strings.TrimSpace(typed.String())
		if text == "" {
			return "", false
		}
		return text, true
	default:
		return "", false
	}
}

func parseImportedExpiry(text string, threshold int64) (time.Time, bool) {
	if n, err := strconv.ParseInt(text, 10, 64); err == nil {
		return expiryFromInt(n, threshold)
	}
	if value, err := strconv.ParseFloat(text, 64); err == nil {
		return expiryFromFloat(value, threshold)
	}
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func expiryFromInt(n, threshold int64) (time.Time, bool) {
	if n <= 0 {
		return time.Time{}, false
	}
	if n >= threshold {
		return time.UnixMilli(n).UTC(), true
	}
	return time.Unix(n, 0).UTC(), true
}

func expiryFromFloat(value float64, threshold int64) (time.Time, bool) {
	// 2^63 is not representable as int64. MaxInt64's float64 conversion rounds
	// up to that power, so the cut rejects every value int64 cannot hold.
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) || value >= float64(math.MaxInt64) {
		return time.Time{}, false
	}
	if value >= float64(threshold) {
		return time.UnixMilli(int64(value)).UTC(), true
	}
	return time.Unix(int64(value), 0).UTC(), true
}

func dedupImportedAccounts(accounts []ImportedAccount) []ImportedAccount {
	seen := make(map[string]struct{}, len(accounts))
	out := make([]ImportedAccount, 0, len(accounts))
	for _, account := range accounts {
		key := importedAccountKey(account)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, account)
	}
	return out
}

func importedAccountKey(account ImportedAccount) string {
	names := make([]string, 0, len(account.Fields))
	for name := range account.Fields {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		value := account.Fields[name]
		fmt.Fprintf(&b, "%d:%s%d:%s|", len(name), name, len(value), value)
	}
	if account.ExpiresAt.IsZero() {
		b.WriteString("exp:0")
		return b.String()
	}
	fmt.Fprintf(&b, "exp:%d.%09d", account.ExpiresAt.Unix(), account.ExpiresAt.Nanosecond())
	return b.String()
}
