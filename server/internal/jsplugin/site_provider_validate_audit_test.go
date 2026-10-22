package jsplugin

import (
	"strings"
	"testing"
)

func TestValidateAccountDisplayRejectsEachBrokenSource(t *testing.T) {
	fields := auditCredentialFields()
	nine := make([]string, maxCredentialSources+1)
	for i := range nine {
		nine[i] = "field.subject"
	}
	cases := []struct {
		name    string
		display CredentialSources
		want    string
	}{
		{name: "too many", display: Sources(nine...), want: "at most"},
		{name: "duplicate", display: Sources("field.subject", "field.subject"), want: "duplicate source"},
		{name: "duplicate summary", display: Sources(summaryLabelSource, summaryLabelSource), want: "duplicate source"},
		{name: "not a source", display: Sources("subject"), want: "is invalid"},
		{name: "bad field name", display: Sources("field.Subject"), want: "is invalid"},
		{name: "undeclared", display: Sources("field.missing"), want: "is not declared"},
		{name: "secret", display: Sources("field.access_token"), want: "must not be secret"},
		{name: "secret among claims", display: Sources("claims.sub", "field.access_token"), want: "must not be secret"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAccountDisplay(tc.display, fields)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validateAccountDisplay(%v) = %v, want %q", tc.display.Items, err, tc.want)
			}
		})
	}
}

func TestValidateAccountDisplayAcceptsDeclaredSources(t *testing.T) {
	fields := auditCredentialFields()
	if err := validateAccountDisplay(CredentialSources{}, fields); err != nil {
		t.Fatalf("empty display: %v", err)
	}
	if err := validateAccountDisplay(Sources("field.subject", summaryLabelSource, "claims.email"), fields); err != nil {
		t.Fatalf("valid display: %v", err)
	}
}

func TestValidateExpiresFromRejectsUnsupportedAndMissingField(t *testing.T) {
	cases := []struct {
		name   string
		value  string
		field  string
		want   string
		wantOK bool
	}{
		{name: "omitted", wantOK: true},
		{name: "claim exp", value: expiresFromClaimExp, field: "expires_at", wantOK: true},
		{name: "unsupported", value: "header.date", field: "expires_at", want: "is not supported"},
		{name: "missing field", value: expiresFromClaimExp, want: "requires expiresAtField"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateExpiresFrom(tc.value, tc.field)
			if tc.wantOK {
				if err != nil {
					t.Fatalf("validateExpiresFrom(%q, %q) = %v", tc.value, tc.field, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validateExpiresFrom(%q, %q) = %v, want %q", tc.value, tc.field, err, tc.want)
			}
		})
	}
}

func TestValidateRegionAttrsRejectsEachBrokenEntry(t *testing.T) {
	cases := []struct {
		name  string
		attrs map[string]string
		want  string
	}{
		{name: "too many", attrs: map[string]string{"origin": "https://example.com", "referer": "https://example.com", "extra": "x"}, want: "at most"},
		{name: "unknown key", attrs: map[string]string{"host": "example.com"}, want: "is not allowed"},
		{name: "empty", attrs: map[string]string{"origin": ""}, want: "without control characters"},
		{name: "too long", attrs: map[string]string{"origin": strings.Repeat("a", maxRegionAttrBytes+1)}, want: "without control characters"},
		{name: "control", attrs: map[string]string{"referer": "https://example.com/\n"}, want: "without control characters"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRegionAttrs(tc.attrs, 0)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validateRegionAttrs(%v) = %v, want %q", tc.attrs, err, tc.want)
			}
		})
	}
	if err := validateRegionAttrs(map[string]string{"origin": "https://example.com", "referer": "https://example.com/app"}, 1); err != nil {
		t.Fatalf("valid attrs: %v", err)
	}
	if err := validateRegionAttrs(nil, 0); err != nil {
		t.Fatalf("no attrs: %v", err)
	}
}

func TestValidateImportValueMapRejectsEachBrokenEntry(t *testing.T) {
	declared := map[string]struct{}{"realm": {}, "expires_at": {}, "subject": {}}
	tooMany := map[string]map[string]string{}
	for i := 0; i < maxImportValueMapFields+1; i++ {
		name := "subject"
		if i > 0 {
			declared["f"+string(rune('a'+i))] = struct{}{}
			name = "f" + string(rune('a'+i))
		}
		tooMany[name] = map[string]string{"intl": "international"}
	}
	tooManyEntries := map[string]string{}
	for i := 0; i < maxImportValueMapEntries+1; i++ {
		tooManyEntries[strings.Repeat("a", i+1)] = "intl"
	}
	cases := []struct {
		name  string
		value map[string]map[string]string
		want  string
	}{
		{name: "too many fields", value: tooMany, want: "at most"},
		{name: "undeclared", value: map[string]map[string]string{"missing": {"a": "b"}}, want: "is not a declared field"},
		{name: "expires field", value: map[string]map[string]string{"expires_at": {"1": "2"}}, want: "cannot rewrite the expires field"},
		{name: "no entries", value: map[string]map[string]string{"realm": {}}, want: "must list 1 to"},
		{name: "too many entries", value: map[string]map[string]string{"realm": tooManyEntries}, want: "must list 1 to"},
		{name: "bad key", value: map[string]map[string]string{"realm": {"bad key": "intl"}}, want: "key:"},
		{name: "empty key", value: map[string]map[string]string{"realm": {"": "intl"}}, want: "key:"},
		{name: "control key", value: map[string]map[string]string{"realm": {"bad\nkey": "intl"}}, want: "key:"},
		{name: "long key", value: map[string]map[string]string{"realm": {strings.Repeat("a", maxImportValueBytes+1): "intl"}}, want: "key:"},
		{name: "bad value", value: map[string]map[string]string{"realm": {"intl": "not a value"}}, want: "value:"},
		{name: "empty value", value: map[string]map[string]string{"realm": {"intl": ""}}, want: "value:"},
		{name: "control value", value: map[string]map[string]string{"realm": {"intl": "bad\n"}}, want: "value:"},
		{name: "long value", value: map[string]map[string]string{"realm": {"intl": strings.Repeat("a", maxImportValueBytes+1)}}, want: "value:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateImportValueMap(tc.value, declared, "expires_at")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validateImportValueMap() = %v, want %q", err, tc.want)
			}
		})
	}
	if err := validateImportValueMap(map[string]map[string]string{"realm": {"intl": "international", "cn": "china"}}, declared, "expires_at"); err != nil {
		t.Fatalf("valid value map: %v", err)
	}
	if err := validateImportValueMap(nil, declared, "expires_at"); err != nil {
		t.Fatalf("empty value map: %v", err)
	}
}

func auditCredentialFields() map[string]credentialFieldMeta {
	return map[string]credentialFieldMeta{
		"access_token": {secret: true, typ: "string"},
		"subject":      {typ: "string"},
		"expires_at":   {typ: "unix"},
	}
}
