package jsplugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"xlyra/server/internal/accountauth"
)

const (
	maxCredentialSources = 8
	summaryLabelSource   = "summary.label"
	expiresFromClaimExp  = "claims.exp"
)

// CredentialSources is an ordered list of identity or display sources.
// JSON is either one string or an array of strings. A single string stays a
// string when the manifest is written back. The first non-empty source wins.
type CredentialSources struct {
	Items []string
}

// Sources builds a source list. Empty items are kept so validation can reject them.
func Sources(items ...string) CredentialSources {
	if len(items) == 0 {
		return CredentialSources{}
	}
	return CredentialSources{Items: append([]string(nil), items...)}
}

// Empty reports whether no source was declared.
func (s CredentialSources) Empty() bool {
	return len(s.Items) == 0
}

// Equal reports whether the list is exactly one source equal to value.
func (s CredentialSources) Equal(value string) bool {
	return len(s.Items) == 1 && s.Items[0] == value
}

// MarshalJSON writes one string, or an array when there are several sources.
func (s CredentialSources) MarshalJSON() ([]byte, error) {
	if len(s.Items) == 1 {
		return json.Marshal(s.Items[0])
	}
	if len(s.Items) == 0 {
		return []byte("null"), nil
	}
	return json.Marshal(s.Items)
}

// UnmarshalJSON accepts a string or an array of strings.
func (s *CredentialSources) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		*s = CredentialSources{}
		return nil
	}
	if data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		s.Items = []string{text}
		return nil
	}
	if data[0] != '[' {
		return errors.New("must be a string or an array of strings")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var items []string
	if err := dec.Decode(&items); err != nil {
		return errors.New("must be a string or an array of strings")
	}
	s.Items = items
	return nil
}

func cloneSources(in CredentialSources) CredentialSources {
	if in.Items == nil {
		return CredentialSources{}
	}
	return CredentialSources{Items: append([]string(nil), in.Items...)}
}

// StoresSummaryLabel reports whether a summarizeAccount label should be kept
// as the account's display text.
func (s AccountPoolSpec) StoresSummaryLabel() bool {
	return s.Credential.Display.has(summaryLabelSource)
}

func (s CredentialSources) has(source string) bool {
	for _, item := range s.Items {
		if item == source {
			return true
		}
	}
	return false
}

// ResolveIdentity returns the first non-empty identity source.
// access is the access token. fields holds the credential bag, including secrets.
func (s AccountPoolSpec) ResolveIdentity(fields map[string]string) (string, error) {
	access := ""
	if fields != nil {
		access = fields[s.accessFieldName()]
	}
	return resolveIdentity(s.Credential.Identity.Items, fields, access, "")
}

// ResolveDisplay returns the first non-empty display source.
// summaryLabel is the scrubbed summarizeAccount label stored on the account.
// An empty result means the caller should fall back to the identity label.
func (s AccountPoolSpec) ResolveDisplay(fields map[string]string, summaryLabel string) string {
	access := ""
	if fields != nil {
		access = fields[s.accessFieldName()]
	}
	value, err := resolveDisplay(s.Credential.Display.Items, fields, access, "", summaryLabel)
	if err != nil {
		return ""
	}
	return value
}

func (s AccountPoolSpec) accessFieldName() string {
	if s.Credential.AccessTokenField != "" {
		return s.Credential.AccessTokenField
	}
	return defaultAccessTokenField
}

func resolveIdentity(candidates []string, fields map[string]string, access, idToken string) (string, error) {
	if len(candidates) == 0 {
		return "", errors.New("account identity is not configured")
	}
	for _, candidate := range candidates {
		value, ok := readCredentialSource(candidate, fields, access, idToken, "")
		if ok {
			return value, nil
		}
	}
	if len(candidates) == 1 {
		if name, ok := strings.CutPrefix(candidates[0], "field."); ok {
			return "", fmt.Errorf("identity field %q is empty", name)
		}
	}
	return "", errors.New("identity claim is empty")
}

func resolveDisplay(candidates []string, fields map[string]string, access, idToken, summaryLabel string) (string, error) {
	for _, candidate := range candidates {
		value, ok := readCredentialSource(candidate, fields, access, idToken, summaryLabel)
		if ok {
			return value, nil
		}
	}
	return "", nil
}

func readCredentialSource(candidate string, fields map[string]string, access, idToken, summaryLabel string) (string, bool) {
	if candidate == summaryLabelSource {
		value := strings.TrimSpace(summaryLabel)
		return value, value != ""
	}
	if name, ok := strings.CutPrefix(candidate, "field."); ok {
		if fields == nil {
			return "", false
		}
		value := strings.TrimSpace(fields[name])
		return value, value != ""
	}
	value, err := accountauth.ClaimString(access, candidate)
	if (err != nil || strings.TrimSpace(value) == "") && idToken != "" {
		value, err = accountauth.ClaimString(idToken, candidate)
	}
	if err != nil {
		return "", false
	}
	value = strings.TrimSpace(value)
	return value, value != ""
}

// FillExpiresFrom sets the unix expiry field from the access token's exp claim
// when the field is empty and expiresFrom is claims.exp. A token response that
// already carried an expiry is left alone.
func FillExpiresFrom(spec AccountPoolSpec, fields map[string]string) {
	name := spec.Credential.ExpiresAtField
	if fields == nil || name == "" || spec.Credential.ExpiresFrom != expiresFromClaimExp {
		return
	}
	if strings.TrimSpace(fields[name]) != "" {
		return
	}
	exp, ok := accountauth.ClaimExpiry(fields[spec.accessFieldName()])
	if !ok || exp.IsZero() || exp.Unix() <= 0 {
		return
	}
	fields[name] = fmt.Sprintf("%d", exp.Unix())
}
