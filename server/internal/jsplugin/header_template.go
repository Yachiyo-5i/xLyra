package jsplugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxSiteProviderRequestHeaders = 16

// HeaderSpec is one upstream header template.
// A JSON string is unconditional. An object is included only when its cred
// field is non-empty (when) or empty (unless). when and unless cannot both be set.
// A failed condition omits the header. There is no general expression.
type HeaderSpec struct {
	Value  string
	When   string
	Unless string
}

// MarshalJSON writes a string when the header is unconditional.
func (h HeaderSpec) MarshalJSON() ([]byte, error) {
	if h.When == "" && h.Unless == "" {
		return json.Marshal(h.Value)
	}
	type wire struct {
		Value  string `json:"value"`
		When   string `json:"when,omitempty"`
		Unless string `json:"unless,omitempty"`
	}
	return json.Marshal(wire{Value: h.Value, When: h.When, Unless: h.Unless})
}

// UnmarshalJSON accepts a string or {"value","when","unless"}.
func (h *HeaderSpec) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		return errors.New("header template is empty")
	}
	if data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		*h = HeaderSpec{Value: text}
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var wire struct {
		Value  string `json:"value"`
		When   string `json:"when"`
		Unless string `json:"unless"`
	}
	if err := dec.Decode(&wire); err != nil {
		return errors.New("header template must be a string or an object with value, when, and unless")
	}
	*h = HeaderSpec{Value: wire.Value, When: wire.When, Unless: wire.Unless}
	return nil
}

// HeaderMap builds unconditional header templates from plain strings.
func HeaderMap(values map[string]string) map[string]HeaderSpec {
	if values == nil {
		return nil
	}
	out := make(map[string]HeaderSpec, len(values))
	for key, value := range values {
		out[key] = HeaderSpec{Value: value}
	}
	return out
}

// HeaderTemplatePiece is one literal or one placeholder from a header template.
// Key is empty for a literal piece. Literal is empty for a placeholder piece.
type HeaderTemplatePiece struct {
	Literal string
	Key     string
}

// SplitHeaderTemplate splits template into literals and placeholders.
// It checks syntax only: {{cred.<field>}}, {{region.<key>}}, and {{site.baseURL}},
// with no spaces, nesting, or unclosed braces. Allow-lists are applied by
// ValidateHeaderTemplate.
func SplitHeaderTemplate(template string) ([]HeaderTemplatePiece, error) {
	return splitHeaderTemplate(template, nil, nil, false)
}

// ValidateHeaderTemplate checks template syntax, {{cred.<field>}} against
// allowed, and {{region.<key>}} against regionKeys. regionKeys is the set of
// attributes declared on every region. A nil or empty set allows no cred
// fields and no region keys. {{site.baseURL}} does not use either set.
func ValidateHeaderTemplate(template string, allowed, regionKeys map[string]struct{}) error {
	_, err := splitHeaderTemplate(template, allowed, regionKeys, true)
	return err
}

// ValidateSiteProviderRequest checks request header names and templates.
// allowed is the cred field set. regionKeys are attributes present on every region.
func ValidateSiteProviderRequest(section SiteProviderRequest, allowed, regionKeys map[string]struct{}) error {
	if len(section.Headers) > maxSiteProviderRequestHeaders {
		return fmt.Errorf("siteProvider.request.headers must list at most %d headers", maxSiteProviderRequestHeaders)
	}
	names := make([]string, 0, len(section.Headers))
	for name := range section.Headers {
		names = append(names, name)
	}
	sort.Strings(names)
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if !validHeaderToken(name) {
			return fmt.Errorf("siteProvider.request.headers: %q is not a valid HTTP token", name)
		}
		lower := strings.ToLower(name)
		if _, ok := seen[lower]; ok {
			return fmt.Errorf("siteProvider.request.headers: duplicate header %q", name)
		}
		seen[lower] = struct{}{}
		if forbiddenRequestHeader(lower) {
			return fmt.Errorf("siteProvider.request.headers: %q is not allowed", name)
		}
		if err := validateHeaderSpec(section.Headers[name], allowed, regionKeys); err != nil {
			return fmt.Errorf("siteProvider.request.headers[%q]: %w", name, err)
		}
	}
	return nil
}

func validateHeaderSpec(spec HeaderSpec, allowed, regionKeys map[string]struct{}) error {
	if spec.When != "" && spec.Unless != "" {
		return errors.New("when and unless are mutually exclusive")
	}
	condition := spec.When
	if condition == "" {
		condition = spec.Unless
	}
	if condition != "" {
		if spec.Value == "" {
			return errors.New("conditional header value is empty")
		}
		field, ok := strings.CutPrefix(condition, "cred.")
		if !ok || !validHeaderTemplateField(field) {
			return fmt.Errorf("condition %q is invalid", condition)
		}
		if _, exists := allowed[field]; !exists {
			return fmt.Errorf("unknown field %q", field)
		}
	}
	return ValidateHeaderTemplate(spec.Value, allowed, regionKeys)
}

func headerTemplateFields(model string) map[string]struct{} {
	switch model {
	case "api_key":
		return map[string]struct{}{"api_key": {}}
	default:
		return map[string]struct{}{}
	}
}

func siteProviderHeaderFields(section SiteProviderSection) map[string]struct{} {
	if section.Credential.Model != "account_pool" {
		return headerTemplateFields(section.Credential.Model)
	}
	allowed := make(map[string]struct{}, len(section.Credential.Fields))
	for _, field := range section.Credential.Fields {
		if field.Name != "" {
			allowed[field.Name] = struct{}{}
		}
	}
	return allowed
}

func siteProviderRequestPresent(section SiteProviderRequest) bool {
	return len(section.Headers) > 0 || section.StripDownstreamHeaders || section.UpstreamStreamOnly || section.BaseURLFrom != ""
}

var forbiddenPluginRequestHeaders = map[string]struct{}{
	"host":                {},
	"content-length":      {},
	"transfer-encoding":   {},
	"connection":          {},
	"upgrade":             {},
	"te":                  {},
	"trailer":             {},
	"proxy-authorization": {},
	"proxy-connection":    {},
	"cookie":              {},
	"set-cookie":          {},
	"content-type":        {},
	"accept":              {},
}

func forbiddenRequestHeader(lowerName string) bool {
	if _, ok := forbiddenPluginRequestHeaders[lowerName]; ok {
		return true
	}
	return strings.HasPrefix(lowerName, "x-forwarded-")
}

func splitHeaderTemplate(template string, allowed, regionKeys map[string]struct{}, enforceFields bool) ([]HeaderTemplatePiece, error) {
	var pieces []HeaderTemplatePiece
	var literal strings.Builder
	flush := func() {
		if literal.Len() == 0 {
			return
		}
		pieces = append(pieces, HeaderTemplatePiece{Literal: literal.String()})
		literal.Reset()
	}
	for i := 0; i < len(template); {
		if template[i] == '{' && i+1 < len(template) && template[i+1] == '{' {
			rest := template[i+2:]
			end := strings.Index(rest, "}}")
			if end < 0 {
				return nil, errors.New("unclosed {{")
			}
			body := rest[:end]
			if strings.Contains(body, "{") || strings.Contains(body, "}") {
				return nil, errors.New("nested placeholder")
			}
			if strings.IndexFunc(body, unicode.IsControl) >= 0 {
				return nil, errors.New("template contains a control character")
			}
			if strings.IndexFunc(body, unicode.IsSpace) >= 0 {
				return nil, errors.New("placeholder must not contain spaces")
			}
			key, err := headerTemplateKey(body, allowed, regionKeys, enforceFields)
			if err != nil {
				return nil, err
			}
			flush()
			pieces = append(pieces, HeaderTemplatePiece{Key: key})
			i += 2 + end + 2
			continue
		}
		r, size := utf8.DecodeRuneInString(template[i:])
		if (r == utf8.RuneError && size == 1) || unicode.IsControl(r) {
			return nil, errors.New("template contains a control character")
		}
		literal.WriteRune(r)
		i += size
	}
	flush()
	return pieces, nil
}

func headerTemplateKey(body string, allowed, regionKeys map[string]struct{}, enforceFields bool) (string, error) {
	if body == "site.baseURL" {
		return "site.baseURL", nil
	}
	if key, ok := strings.CutPrefix(body, "region."); ok {
		if _, allowedKey := regionAttrKeys[key]; !allowedKey || !validHeaderTemplateField(key) {
			return "", fmt.Errorf("unknown placeholder %q", "{{"+body+"}}")
		}
		if enforceFields {
			if _, ok := regionKeys[key]; !ok {
				return "", fmt.Errorf("unknown region attribute %q", key)
			}
		}
		return "region." + key, nil
	}
	field, ok := strings.CutPrefix(body, "cred.")
	if !ok || !validHeaderTemplateField(field) {
		return "", fmt.Errorf("unknown placeholder %q", "{{"+body+"}}")
	}
	if enforceFields {
		if _, ok := allowed[field]; !ok {
			return "", fmt.Errorf("unknown field %q", field)
		}
	}
	return "cred." + field, nil
}

func validHeaderTemplateField(field string) bool {
	if field == "" {
		return false
	}
	for i := 0; i < len(field); i++ {
		c := field[i]
		if i == 0 {
			if c != '_' && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
				return false
			}
			continue
		}
		if c != '_' && (c < '0' || c > '9') && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
			return false
		}
	}
	return true
}
