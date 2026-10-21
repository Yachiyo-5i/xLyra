package gateway

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"unicode"

	"xlyra/server/internal/jsplugin"
	"xlyra/server/internal/site"
)

// RenderHeaderTemplate replaces {{cred.<field>}}, {{region.<key>}}, and {{site.baseURL}} using vars.
// A missing variable is an error. The rendered text must not contain CR, LF,
// or any other control character.
func RenderHeaderTemplate(template string, vars map[string]string) (string, error) {
	pieces, err := jsplugin.SplitHeaderTemplate(template)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, piece := range pieces {
		if piece.Key == "" {
			b.WriteString(piece.Literal)
			continue
		}
		value, ok := vars[piece.Key]
		if !ok {
			return "", fmt.Errorf("missing variable %s", piece.Key)
		}
		b.WriteString(value)
	}
	rendered := b.String()
	if headerValueHasControl(rendered) {
		return "", fmt.Errorf("value contains a control character")
	}
	return rendered, nil
}

func headerValueHasControl(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 || value[i] == 0x7f {
			return true
		}
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// pluginHeaderTemplateVars builds the variable map for one upstream call.
// A nil headerVars means an API-key site: cred.api_key is the decrypted key.
// A non-nil map is used as given (account-pool resolvers fill their own fields).
// site.baseURL is always the base URL this request uses, without a trailing slash.
func pluginHeaderTemplateVars(headerVars map[string]string, upstreamKey, baseURL string) map[string]string {
	vars := make(map[string]string, len(headerVars)+2)
	for key, value := range headerVars {
		vars[key] = value
	}
	if headerVars == nil {
		vars["cred.api_key"] = upstreamKey
	}
	vars["site.baseURL"] = strings.TrimRight(baseURL, "/")
	return vars
}

// applyPluginRequestHeaders renders a plugin site type's header template onto req.
// It runs after protocol headers, downstream passthrough, and built-in site
// headers, and before the request is signed. Built-in site types and plugin
// types with no request config are left unchanged.
func applyPluginRequestHeaders(req *http.Request, siteType string, headerVars map[string]string, upstreamKey, baseURL string) error {
	if req == nil {
		return nil
	}
	item, ok := site.LookupPluginSiteType(siteType)
	if !ok || (len(item.Request.Headers) == 0 && !item.Request.StripDownstreamHeaders) {
		return nil
	}
	names := make([]string, 0, len(item.Request.Headers))
	for name := range item.Request.Headers {
		names = append(names, name)
	}
	sort.Strings(names)
	vars := pluginHeaderTemplateVars(headerVars, upstreamKey, baseURL)
	rendered := make([]renderedPluginHeader, 0, len(names))
	var omitted []string
	for _, name := range names {
		spec := item.Request.Headers[name]
		if !headerSpecIncluded(spec, vars) {
			omitted = append(omitted, name)
			continue
		}
		value, err := RenderHeaderTemplate(spec.Value, vars)
		if err != nil {
			return fmt.Errorf("header %s: %s", name, err.Error())
		}
		rendered = append(rendered, renderedPluginHeader{name: name, value: value})
	}
	if item.Request.StripDownstreamHeaders {
		stripPluginDownstreamHeaders(req, item.Request.Headers)
	}
	for _, name := range omitted {
		req.Header.Del(name)
	}
	for _, header := range rendered {
		req.Header.Set(header.name, header.value)
	}
	return nil
}

// headerSpecIncluded reports whether a header should be sent.
// A missing variable counts as empty. A failed condition omits the header
// instead of rendering an empty value.
func headerSpecIncluded(spec jsplugin.HeaderSpec, vars map[string]string) bool {
	if spec.When == "" && spec.Unless == "" {
		return true
	}
	if spec.When != "" {
		return strings.TrimSpace(vars[spec.When]) != ""
	}
	return strings.TrimSpace(vars[spec.Unless]) == ""
}

type renderedPluginHeader struct {
	name  string
	value string
}

func stripPluginDownstreamHeaders(req *http.Request, templates map[string]jsplugin.HeaderSpec) {
	keep := map[string]struct{}{
		"Content-Type": {},
		"Accept":       {},
	}
	for name := range templates {
		keep[http.CanonicalHeaderKey(name)] = struct{}{}
	}
	for name := range req.Header {
		if _, ok := keep[name]; !ok {
			req.Header.Del(name)
		}
	}
}
