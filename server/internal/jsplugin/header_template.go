package jsplugin

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxSiteProviderRequestHeaders = 16

// HeaderTemplatePiece is one literal or one placeholder from a header template.
// Key is empty for a literal piece. Literal is empty for a placeholder piece.
type HeaderTemplatePiece struct {
	Literal string
	Key     string
}

// SplitHeaderTemplate splits template into literals and placeholders.
// It checks syntax only: {{cred.<field>}} and {{site.baseURL}}, with no spaces,
// nesting, or unclosed braces. The cred field allow-list is applied by
// ValidateHeaderTemplate.
func SplitHeaderTemplate(template string) ([]HeaderTemplatePiece, error) {
	return splitHeaderTemplate(template, nil, false)
}

// ValidateHeaderTemplate checks template syntax and that every {{cred.<field>}}
// uses a field in allowed. allowed is the credential model's field set.
// A nil or empty set allows no cred fields. {{site.baseURL}} does not use allowed.
func ValidateHeaderTemplate(template string, allowed map[string]struct{}) error {
	_, err := splitHeaderTemplate(template, allowed, true)
	return err
}

// ValidateSiteProviderRequest checks request header names and templates.
// allowed is the cred field set passed to ValidateHeaderTemplate.
func ValidateSiteProviderRequest(section SiteProviderRequest, allowed map[string]struct{}) error {
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
		if err := ValidateHeaderTemplate(section.Headers[name], allowed); err != nil {
			return fmt.Errorf("siteProvider.request.headers[%q]: %w", name, err)
		}
	}
	return nil
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

func splitHeaderTemplate(template string, allowed map[string]struct{}, enforceFields bool) ([]HeaderTemplatePiece, error) {
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
			key, err := headerTemplateKey(body, allowed, enforceFields)
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

func headerTemplateKey(body string, allowed map[string]struct{}, enforceFields bool) (string, error) {
	if body == "site.baseURL" {
		return "site.baseURL", nil
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
