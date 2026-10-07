package jsplugin

import (
	"fmt"
	"net/textproto"
	"net/url"
	"strings"
)

var droppedRequestHeaders = map[string]struct{}{
	"authorization":       {},
	"proxy-authorization": {},
	"cookie":              {},
	"host":                {},
	"content-length":      {},
	"transfer-encoding":   {},
	"connection":          {},
}

// ResolveProbeURL joins a plugin path onto the canonical site base and
// rejects anything that leaves that origin.
func ResolveProbeURL(base, path string, query map[string]string) (string, error) {
	if err := validatePluginPath(path); err != nil {
		return "", err
	}
	baseURL, err := url.Parse(base)
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return "", fmt.Errorf("invalid base URL")
	}
	pathOnly, pathQuery, _ := strings.Cut(path, "?")
	joined := strings.TrimRight(base, "/") + pathOnly
	resolved, err := url.Parse(joined)
	if err != nil {
		return "", err
	}
	if resolved.User != nil {
		return "", fmt.Errorf("url userinfo is not allowed")
	}
	if !sameOrigin(baseURL, resolved) {
		return "", fmt.Errorf("url is outside the site origin")
	}
	values := url.Values{}
	if pathQuery != "" {
		parsed, err := url.ParseQuery(pathQuery)
		if err != nil {
			return "", err
		}
		for key, items := range parsed {
			for _, item := range items {
				values.Add(key, item)
			}
		}
	}
	for key, value := range query {
		values.Set(key, value)
	}
	resolved.RawQuery = values.Encode()
	resolved.Fragment = ""
	return resolved.String(), nil
}

// SameOrigin reports whether raw is on the same scheme, host, and port as base.
func SameOrigin(base, raw string) error {
	baseURL, err := url.Parse(base)
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return fmt.Errorf("invalid base URL")
	}
	next, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if next.User != nil {
		return fmt.Errorf("url userinfo is not allowed")
	}
	if !sameOrigin(baseURL, next) {
		return fmt.Errorf("url is outside the site origin")
	}
	return nil
}

func sameOrigin(base, next *url.URL) bool {
	return strings.EqualFold(base.Scheme, next.Scheme) && strings.EqualFold(base.Host, next.Host)
}

func validatePluginPath(path string) error {
	if path == "" || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return fmt.Errorf("path must start with a single /")
	}
	if strings.Contains(path, `\`) || strings.Contains(path, "#") || strings.Contains(path, "://") {
		return fmt.Errorf("path is not allowed")
	}
	raw, _, _ := strings.Cut(path, "?")
	for _, segment := range strings.Split(raw, "/") {
		if segment == "" {
			continue
		}
		if segment == "." || segment == ".." {
			return fmt.Errorf("path is not allowed")
		}
		decoded, err := url.PathUnescape(segment)
		if err != nil {
			return err
		}
		if decoded == "." || decoded == ".." || strings.ContainsAny(decoded, `/\\`) {
			return fmt.Errorf("path is not allowed")
		}
	}
	return nil
}

// FilterRequestHeaders drops credential and hop-by-hop headers. The caller
// injects Authorization after this returns.
func FilterRequestHeaders(in map[string]string) (map[string]string, error) {
	out := map[string]string{}
	total := 0
	for key, value := range in {
		name := strings.ToLower(strings.TrimSpace(key))
		if name == "" {
			continue
		}
		if _, drop := droppedRequestHeaders[name]; drop || strings.HasPrefix(name, "x-forwarded-") {
			continue
		}
		if len(out) >= maxRequestHeaders {
			return nil, fmt.Errorf("too many request headers")
		}
		total += len(key) + len(value)
		if total > maxRequestHeaderBytes {
			return nil, fmt.Errorf("request headers are too large")
		}
		out[textproto.CanonicalMIMEHeaderKey(key)] = value
	}
	return out, nil
}

// FilterResponseHeaders removes Set-Cookie before a response is shown to a plugin.
func FilterResponseHeaders(in map[string][]string) map[string]string {
	out := map[string]string{}
	for key, values := range in {
		if strings.EqualFold(key, "Set-Cookie") {
			continue
		}
		copied := make([]string, 0, len(values))
		for _, value := range values {
			copied = append(copied, value)
		}
		out[textproto.CanonicalMIMEHeaderKey(key)] = strings.Join(copied, ", ")
	}
	return out
}
