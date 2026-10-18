package gateway

import (
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"
)

// Native (provider hosted) web search support.
//
// Hosted web search is not a function call: every vendor declares it with a
// different tool shape and executes it server side. The gateway keeps a single
// canonical representation (canonicalTool{Type: "web_search"} carrying a
// webSearchSpec) and translates it to the native shape of the upstream
// protocol when the upstream model is known to support it.

const (
	canonicalWebSearchType = "web_search"
	// anthropicWebSearchToolType is the server tool version we emit upstream.
	anthropicWebSearchToolType = "web_search_20250305"
)

type webSearchSpec struct {
	AllowedDomains []string
	BlockedDomains []string
	MaxUses        int
	ContextSize    string
	UserLocation   map[string]any
	// ResponsesRaw keeps the original Responses tool so Responses -> Responses
	// conversions do not lose vendor specific fields.
	ResponsesRaw map[string]any
}

type canonicalCitation struct {
	URL        string
	Title      string
	CitedText  string
	StartIndex int
	EndIndex   int
	HasRange   bool
}

func isWebSearchToolType(toolType string) bool {
	toolType = strings.ToLower(strings.TrimSpace(toolType))
	return toolType == canonicalWebSearchType || strings.HasPrefix(toolType, "web_search_") || toolType == "web_search_preview"
}

func canonicalToolIsWebSearch(tool canonicalTool) bool {
	return isWebSearchToolType(tool.Type)
}

func canonicalToolsHaveWebSearch(tools []canonicalTool) bool {
	for _, tool := range tools {
		if canonicalToolIsWebSearch(tool) {
			return true
		}
	}
	return false
}

func stringSliceFromAny(raw any) []string {
	items, ok := raw.([]any)
	if !ok {
		if typed, ok := raw.([]string); ok {
			return append([]string(nil), typed...)
		}
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if value := strings.TrimSpace(anyString(item)); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func webSearchSpecFromMap(tool map[string]any) webSearchSpec {
	spec := webSearchSpec{}
	if tool == nil {
		return spec
	}
	if filters, ok := tool["filters"].(map[string]any); ok {
		spec.AllowedDomains = stringSliceFromAny(filters["allowed_domains"])
	}
	if len(spec.AllowedDomains) == 0 {
		spec.AllowedDomains = stringSliceFromAny(tool["allowed_domains"])
	}
	spec.BlockedDomains = stringSliceFromAny(tool["blocked_domains"])
	if value, ok := intFromAny(tool["max_uses"]); ok && value > 0 {
		spec.MaxUses = value
	}
	spec.ContextSize = strings.TrimSpace(anyString(tool["search_context_size"]))
	if location, ok := tool["user_location"].(map[string]any); ok {
		loc := map[string]any{}
		source := location
		if approx, ok := location["approximate"].(map[string]any); ok {
			source = approx
		}
		for _, key := range []string{"country", "city", "region", "timezone"} {
			if value := strings.TrimSpace(anyString(source[key])); value != "" {
				loc[key] = value
			}
		}
		if len(loc) > 0 {
			spec.UserLocation = loc
		}
	}
	return spec
}

// canonicalWebSearchToolFromMap builds the canonical web_search tool from a
// Responses / Anthropic / Chat style tool object.
func canonicalWebSearchToolFromMap(tool map[string]any, fromResponses bool) canonicalTool {
	spec := webSearchSpecFromMap(tool)
	if fromResponses && tool != nil {
		spec.ResponsesRaw = clonePayload(tool)
	}
	return canonicalTool{Type: canonicalWebSearchType, Name: canonicalWebSearchType, Parameters: spec, Raw: tool}
}

func canonicalWebSearchToolFromGemini() canonicalTool {
	return canonicalTool{Type: canonicalWebSearchType, Name: canonicalWebSearchType, Parameters: webSearchSpec{}}
}

func webSearchSpecOfTool(tool canonicalTool) webSearchSpec {
	if spec, ok := tool.Parameters.(webSearchSpec); ok {
		return spec
	}
	spec := webSearchSpecFromMap(tool.Raw)
	if tool.Raw != nil {
		spec.ResponsesRaw = clonePayload(tool.Raw)
	}
	return spec
}

func webSearchUserLocationPayload(spec webSearchSpec) map[string]any {
	if len(spec.UserLocation) == 0 {
		return nil
	}
	out := map[string]any{"type": "approximate"}
	for key, value := range spec.UserLocation {
		out[key] = value
	}
	return out
}

func encodeWebSearchToolAsResponses(tool canonicalTool) map[string]any {
	spec := webSearchSpecOfTool(tool)
	if spec.ResponsesRaw != nil && isWebSearchToolType(anyString(spec.ResponsesRaw["type"])) {
		return spec.ResponsesRaw
	}
	out := map[string]any{"type": canonicalWebSearchType}
	if len(spec.AllowedDomains) > 0 {
		domains := make([]any, 0, len(spec.AllowedDomains))
		for _, domain := range spec.AllowedDomains {
			domains = append(domains, domain)
		}
		out["filters"] = map[string]any{"allowed_domains": domains}
	}
	if loc := webSearchUserLocationPayload(spec); loc != nil {
		out["user_location"] = loc
	}
	if spec.ContextSize != "" {
		out["search_context_size"] = spec.ContextSize
	}
	return out
}

func encodeWebSearchToolAsAnthropic(tool canonicalTool) map[string]any {
	spec := webSearchSpecOfTool(tool)
	out := map[string]any{"type": anthropicWebSearchToolType, "name": "web_search"}
	if spec.MaxUses > 0 {
		out["max_uses"] = spec.MaxUses
	}
	if len(spec.AllowedDomains) > 0 {
		out["allowed_domains"] = spec.AllowedDomains
	} else if len(spec.BlockedDomains) > 0 {
		// Anthropic rejects allowed_domains and blocked_domains together.
		out["blocked_domains"] = spec.BlockedDomains
	}
	if loc := webSearchUserLocationPayload(spec); loc != nil {
		out["user_location"] = loc
	}
	return out
}

func encodeWebSearchOptionsForChat(tool canonicalTool) map[string]any {
	spec := webSearchSpecOfTool(tool)
	out := map[string]any{}
	if spec.ContextSize != "" {
		out["search_context_size"] = spec.ContextSize
	}
	if len(spec.UserLocation) > 0 {
		out["user_location"] = map[string]any{"type": "approximate", "approximate": spec.UserLocation}
	}
	return out
}

func webSearchToolFromChatOptions(options map[string]any) canonicalTool {
	spec := webSearchSpecFromMap(options)
	return canonicalTool{Type: canonicalWebSearchType, Name: canonicalWebSearchType, Parameters: spec}
}

// ---------------------------------------------------------------------------
// Upstream capability table (built in, no user configuration).
// ---------------------------------------------------------------------------

type webSearchSupport int

const (
	webSearchUnsupported webSearchSupport = iota
	// webSearchNative: the upstream accepts a hosted search tool declaration.
	webSearchNative
	// webSearchInherent: the model always searches (e.g. Perplexity sonar), so
	// the declaration is redundant and can be dropped without losing the feature.
	webSearchInherent
)

func webSearchSupportFor(protocol canonicalProtocol, upstreamModel string) webSearchSupport {
	model := strings.ToLower(strings.TrimSpace(upstreamModel))
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		model = model[idx+1:]
	}
	switch protocol {
	case canonicalProtocolOpenAIResponses, canonicalProtocolCodexResponses:
		// Responses upstreams keep their previous pass-through behaviour.
		return webSearchNative
	case canonicalProtocolOpenAIChat:
		switch {
		case strings.Contains(model, "sonar"):
			return webSearchInherent
		case strings.HasPrefix(model, "gpt-") && (strings.Contains(model, "search-preview") || strings.Contains(model, "search-api")):
			return webSearchNative
		}
	case canonicalProtocolAnthropicMessages:
		if !strings.HasPrefix(model, "claude-") {
			return webSearchUnsupported
		}
		for _, legacy := range []string{"claude-2", "claude-instant", "claude-3-opus", "claude-3-sonnet", "claude-3-haiku"} {
			if strings.HasPrefix(model, legacy) {
				return webSearchUnsupported
			}
		}
		return webSearchNative
	case canonicalProtocolGoogleGemini, canonicalProtocolAntigravity:
		if !strings.HasPrefix(model, "gemini-") {
			return webSearchUnsupported
		}
		for _, blocked := range []string{"image", "tts", "embedding", "live", "native-audio", "gemini-1."} {
			if strings.Contains(model, blocked) {
				return webSearchUnsupported
			}
		}
		return webSearchNative
	}
	return webSearchUnsupported
}

// filterWebSearchTools drops web_search tools the upstream cannot honour and
// logs the decision so the capability loss is never silent.
func filterWebSearchTools(tools []canonicalTool, protocol canonicalProtocol, upstreamModel string) []canonicalTool {
	if !canonicalToolsHaveWebSearch(tools) {
		return tools
	}
	support := webSearchSupportFor(protocol, upstreamModel)
	if support == webSearchNative {
		// A request may only carry one hosted search declaration.
		out := make([]canonicalTool, 0, len(tools))
		seen := false
		for _, tool := range tools {
			if canonicalToolIsWebSearch(tool) {
				if seen {
					continue
				}
				seen = true
			}
			out = append(out, tool)
		}
		return out
	}
	out := make([]canonicalTool, 0, len(tools))
	for _, tool := range tools {
		if !canonicalToolIsWebSearch(tool) {
			out = append(out, tool)
		}
	}
	if support == webSearchUnsupported {
		slog.Info("native web_search dropped: upstream model does not support it",
			"protocol", string(protocol), "upstream_model", upstreamModel)
	}
	return out
}

// ---------------------------------------------------------------------------
// Rune / byte offset helpers (Gemini uses UTF-8 byte offsets).
// ---------------------------------------------------------------------------

func byteOffsetToRuneOffset(text string, byteOffset int) int {
	if byteOffset <= 0 {
		return 0
	}
	if byteOffset > len(text) {
		byteOffset = len(text)
	}
	return utf8.RuneCountInString(text[:byteOffset])
}

func runeOffsetToByteOffset(text string, runeOffset int) int {
	if runeOffset <= 0 {
		return 0
	}
	count := 0
	for idx := range text {
		if count == runeOffset {
			return idx
		}
		count++
	}
	return len(text)
}

// citationRange returns the rune range of a citation inside text, defaulting
// to the whole text when the source protocol carried no offsets.
func citationRange(citation canonicalCitation, text string) (int, int) {
	if citation.HasRange {
		return citation.StartIndex, citation.EndIndex
	}
	return 0, utf8.RuneCountInString(text)
}

func citedTextOrTitle(citation canonicalCitation) string {
	text := strings.TrimSpace(citation.CitedText)
	if text == "" {
		text = strings.TrimSpace(citation.Title)
	}
	if text == "" {
		text = citation.URL
	}
	return text
}

func webSearchCallQuery(item canonicalOutputItem) string {
	return strings.Join(item.SearchQueries, "; ")
}

// webSearchHistoryText folds a hosted search call into plain text for
// protocols that cannot replay it (cross-protocol history).
func webSearchHistoryText(item canonicalOutputItem) string {
	query := webSearchCallQuery(item)
	if query == "" && len(item.SearchSources) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("[web_search]")
	if query != "" {
		fmt.Fprintf(&b, " query: %s", query)
	}
	for _, source := range item.SearchSources {
		if source.URL == "" {
			continue
		}
		if source.Title != "" {
			fmt.Fprintf(&b, "\n- %s (%s)", source.Title, source.URL)
		} else {
			fmt.Fprintf(&b, "\n- %s", source.URL)
		}
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Response encoders shared across protocols.
// ---------------------------------------------------------------------------

func responsesAnnotationsFromCitations(citations []canonicalCitation, text string) []any {
	out := make([]any, 0, len(citations))
	for _, citation := range citations {
		if citation.URL == "" {
			continue
		}
		start, end := citationRange(citation, text)
		out = append(out, map[string]any{
			"type":        "url_citation",
			"url":         citation.URL,
			"title":       citation.Title,
			"start_index": start,
			"end_index":   end,
		})
	}
	return out
}

func chatAnnotationsFromCitations(citations []canonicalCitation, text string) []any {
	out := make([]any, 0, len(citations))
	for _, citation := range citations {
		if citation.URL == "" {
			continue
		}
		start, end := citationRange(citation, text)
		out = append(out, map[string]any{
			"type": "url_citation",
			"url_citation": map[string]any{
				"url":         citation.URL,
				"title":       citation.Title,
				"start_index": start,
				"end_index":   end,
			},
		})
	}
	return out
}

func anthropicCitationsFromCanonical(citations []canonicalCitation) []any {
	out := make([]any, 0, len(citations))
	for _, citation := range citations {
		if citation.URL == "" {
			continue
		}
		out = append(out, map[string]any{
			"type":            "web_search_result_location",
			"url":             citation.URL,
			"title":           citation.Title,
			"cited_text":      citedTextOrTitle(citation),
			"encrypted_index": "",
		})
	}
	return out
}

// geminiGroundingMetadata builds Gemini groundingMetadata for a response.
func geminiGroundingMetadata(output []canonicalOutputItem) map[string]any {
	queries := make([]any, 0)
	chunks := make([]any, 0)
	chunkIndex := map[string]int{}
	addChunk := func(citation canonicalCitation) int {
		if idx, ok := chunkIndex[citation.URL]; ok {
			return idx
		}
		idx := len(chunks)
		chunkIndex[citation.URL] = idx
		chunks = append(chunks, map[string]any{"web": map[string]any{"uri": citation.URL, "title": citation.Title}})
		return idx
	}
	supports := make([]any, 0)
	for _, item := range output {
		if item.Type == "web_search_call" {
			for _, query := range item.SearchQueries {
				queries = append(queries, query)
			}
			for _, source := range item.SearchSources {
				if source.URL != "" {
					addChunk(source)
				}
			}
			continue
		}
		if len(item.Citations) == 0 {
			continue
		}
		text := item.Text
		if text == "" {
			for _, part := range item.Content {
				text += part.Text
			}
		}
		for _, citation := range item.Citations {
			if citation.URL == "" {
				continue
			}
			start, end := citationRange(citation, text)
			startByte, endByte := runeOffsetToByteOffset(text, start), runeOffsetToByteOffset(text, end)
			supports = append(supports, map[string]any{
				"segment": map[string]any{
					"startIndex": startByte,
					"endIndex":   endByte,
					"text":       text[startByte:endByte],
				},
				"groundingChunkIndices": []any{addChunk(citation)},
			})
		}
	}
	if len(queries) == 0 && len(chunks) == 0 {
		return nil
	}
	meta := map[string]any{}
	if len(queries) > 0 {
		meta["webSearchQueries"] = queries
	}
	if len(chunks) > 0 {
		meta["groundingChunks"] = chunks
	}
	if len(supports) > 0 {
		meta["groundingSupports"] = supports
	}
	return meta
}

// webSearchItemsFromGemini converts candidate.groundingMetadata into a
// web_search_call item plus citations for the generated text.
func webSearchItemsFromGemini(gemini map[string]any, text string) (*canonicalOutputItem, []canonicalCitation) {
	candidates, _ := gemini["candidates"].([]any)
	if len(candidates) == 0 {
		return nil, nil
	}
	candidate, _ := candidates[0].(map[string]any)
	meta, _ := candidate["groundingMetadata"].(map[string]any)
	if len(meta) == 0 {
		return nil, nil
	}
	queries := stringSliceFromAny(meta["webSearchQueries"])
	chunks, _ := meta["groundingChunks"].([]any)
	sources := make([]canonicalCitation, 0, len(chunks))
	for _, raw := range chunks {
		chunk, _ := raw.(map[string]any)
		web, _ := chunk["web"].(map[string]any)
		uri := strings.TrimSpace(anyString(web["uri"]))
		if uri == "" {
			sources = append(sources, canonicalCitation{})
			continue
		}
		sources = append(sources, canonicalCitation{URL: uri, Title: strings.TrimSpace(anyString(web["title"]))})
	}
	citations := make([]canonicalCitation, 0)
	supports, _ := meta["groundingSupports"].([]any)
	for _, raw := range supports {
		support, _ := raw.(map[string]any)
		segment, _ := support["segment"].(map[string]any)
		start, _ := intFromAny(segment["startIndex"])
		end, hasEnd := intFromAny(segment["endIndex"])
		indices, _ := support["groundingChunkIndices"].([]any)
		for _, rawIndex := range indices {
			idx, ok := intFromAny(rawIndex)
			if !ok || idx < 0 || idx >= len(sources) || sources[idx].URL == "" {
				continue
			}
			citation := sources[idx]
			citation.CitedText = strings.TrimSpace(anyString(segment["text"]))
			if hasEnd {
				citation.HasRange = true
				citation.StartIndex = byteOffsetToRuneOffset(text, start)
				citation.EndIndex = byteOffsetToRuneOffset(text, end)
			}
			citations = append(citations, citation)
		}
	}
	realSources := make([]canonicalCitation, 0, len(sources))
	for _, source := range sources {
		if source.URL != "" {
			realSources = append(realSources, source)
		}
	}
	if len(queries) == 0 && len(realSources) == 0 {
		return nil, nil
	}
	item := &canonicalOutputItem{
		ID:            "ws_" + synthesizedResponseID(),
		Type:          "web_search_call",
		Status:        "completed",
		SearchQueries: queries,
		SearchSources: realSources,
	}
	return item, citations
}

func canonicalCitationsFromResponsesAnnotations(annotations []any) []canonicalCitation {
	out := make([]canonicalCitation, 0, len(annotations))
	for _, raw := range annotations {
		annotation, _ := raw.(map[string]any)
		if anyString(annotation["type"]) != "url_citation" {
			continue
		}
		source := annotation
		if nested, ok := annotation["url_citation"].(map[string]any); ok {
			source = nested
		}
		url := strings.TrimSpace(anyString(source["url"]))
		if url == "" {
			continue
		}
		citation := canonicalCitation{URL: url, Title: strings.TrimSpace(anyString(source["title"]))}
		start, hasStart := intFromAny(source["start_index"])
		end, hasEnd := intFromAny(source["end_index"])
		if hasStart && hasEnd {
			citation.HasRange, citation.StartIndex, citation.EndIndex = true, start, end
		}
		out = append(out, citation)
	}
	return out
}

func webSearchFromResponsesAction(action map[string]any) ([]string, []canonicalCitation) {
	if action == nil {
		return nil, nil
	}
	queries := stringSliceFromAny(action["queries"])
	if len(queries) == 0 {
		if query := strings.TrimSpace(anyString(action["query"])); query != "" {
			queries = []string{query}
		}
	}
	sources := make([]canonicalCitation, 0)
	if items, ok := action["sources"].([]any); ok {
		for _, raw := range items {
			source, _ := raw.(map[string]any)
			if url := strings.TrimSpace(anyString(source["url"])); url != "" {
				sources = append(sources, canonicalCitation{URL: url, Title: strings.TrimSpace(anyString(source["title"]))})
			}
		}
	}
	return queries, sources
}

func responsesWebSearchCallItem(item canonicalOutputItem, id string) map[string]any {
	action := map[string]any{"type": "search", "query": webSearchCallQuery(item)}
	if len(item.SearchSources) > 0 {
		sources := make([]any, 0, len(item.SearchSources))
		for _, source := range item.SearchSources {
			sources = append(sources, map[string]any{"type": "url", "url": source.URL})
		}
		action["sources"] = sources
	}
	return map[string]any{
		"id":     id,
		"type":   "web_search_call",
		"status": nonEmptyString(item.Status, "completed"),
		"action": action,
	}
}

func canonicalWebSearchCallCount(output []canonicalOutputItem) int {
	count := 0
	for _, item := range output {
		if item.Type == "web_search_call" {
			count++
		}
	}
	return count
}

// anthropicToolChoiceTargetsKnownTool guards a forced web_search choice: if the
// search tool was dropped (unsupported upstream) the choice must be dropped too,
// otherwise Anthropic rejects the request.
func anthropicToolChoiceTargetsKnownTool(choice any, tools []any) bool {
	value, _ := choice.(map[string]any)
	if value == nil || anyString(value["type"]) != "tool" || anyString(value["name"]) != "web_search" {
		return true
	}
	for _, raw := range tools {
		tool, _ := raw.(map[string]any)
		if isWebSearchToolType(anyString(tool["type"])) {
			return true
		}
		if anyString(tool["name"]) == "web_search" {
			return true
		}
	}
	return false
}
