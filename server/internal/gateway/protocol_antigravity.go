package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
)

type antigravityProtocolAdapter struct {
	db                  *store.Store
	stream              bool
	downstreamProtocol  canonicalProtocol
	downstreamImages    bool
	imageResponseFormat string
}

func newAntigravityProtocolAdapter(request gatewayRequest, db *store.Store) gatewayProtocolAdapter {
	return antigravityProtocolAdapter{
		db:                  db,
		stream:              request.Stream,
		downstreamProtocol:  downstreamCanonicalProtocol(request.DownstreamPath),
		downstreamImages:    request.DownstreamPath == gatewayEndpointImagesGenerations,
		imageResponseFormat: strings.TrimSpace(anyString(request.Payload["response_format"])),
	}
}

func (a antigravityProtocolAdapter) ProtocolName() string {
	if a.downstreamImages {
		return "antigravity_image_generate_content"
	}
	if a.stream {
		return "antigravity_stream_generate_content"
	}
	return "antigravity_generate_content"
}

func (a antigravityProtocolAdapter) BuildUpstreamPayload(request gatewayRequest, candidate routeengine.Candidate) (map[string]any, error) {
	projectID := a.projectID(request, candidate)
	if projectID == "" {
		return nil, fmt.Errorf("antigravity project_id is missing; refresh the OAuth connection first")
	}
	if a.downstreamImages {
		if request.Canonical != nil {
			return antigravityCanonicalImagePayload(*request.Canonical, candidate.Model.UpstreamName, projectID), nil
		}
		canonical := canonicalRequestFromOpenAIImagesPayload(request.Payload, stringFromPayloadModel(request.Payload))
		return antigravityCanonicalImagePayload(canonical, candidate.Model.UpstreamName, projectID), nil
	}
	var canonical canonicalRequest
	if request.Canonical != nil {
		canonical = *request.Canonical
	} else if request.DownstreamPath == gatewayEndpointResponses {
		decoded, err := canonicalRequestFromOpenAIResponsesPayload(request.Payload, stringFromPayloadModel(request.Payload))
		if err != nil {
			return nil, err
		}
		canonical = decoded
	} else {
		decoded, err := canonicalRequestFromOpenAIChatPayload(request.Payload, stringFromPayloadModel(request.Payload))
		if err != nil {
			return nil, err
		}
		canonical = decoded
	}
	inner := encodeCanonicalRequestToAntigravityGemini(canonical, candidate.Model.UpstreamName)
	return map[string]any{
		"project":            projectID,
		"requestId":          antigravityRequestID(),
		"request":            inner,
		"model":              candidate.Model.UpstreamName,
		"userAgent":          "antigravity",
		"requestType":        "agent",
		"enabledCreditTypes": []string{"GOOGLE_ONE_AI"},
	}, nil
}

func (a antigravityProtocolAdapter) UpstreamPath(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = "https://daily-cloudcode-pa.sandbox.googleapis.com"
	}
	if strings.HasSuffix(base, "/v1internal") {
		if a.stream {
			return base + ":streamGenerateContent?alt=sse"
		}
		return base + ":generateContent"
	}
	if a.stream {
		return base + "/v1internal:streamGenerateContent?alt=sse"
	}
	return base + "/v1internal:generateContent"
}

func (a antigravityProtocolAdapter) TransformBufferedResponse(statusCode int, headers http.Header, body []byte) (gatewayBufferedResponse, error) {
	contentType := strings.TrimSpace(headers.Get("Content-Type"))
	if statusCode < 200 || statusCode >= 300 {
		return gatewayBufferedResponse{
			StatusCode:  statusCode,
			ContentType: contentType,
			Body:        body,
		}, nil
	}

	if a.downstreamImages {
		body, convertedUsage, err := convertResponseBetweenProtocols(canonicalProtocolAntigravity, canonicalProtocolOpenAIImages, body, responseConversionOptions{
			ImageResponseFormat: a.imageResponseFormat,
		})
		if err != nil {
			return gatewayBufferedResponse{}, err
		}
		return gatewayBufferedResponse{
			StatusCode:  statusCode,
			ContentType: "application/json",
			Body:        body,
			Usage:       convertedUsage,
		}, nil
	}
	target := a.downstreamProtocol
	if target == "" || target == canonicalProtocolOpenAIImages {
		target = canonicalProtocolOpenAIChat
	}
	body, convertedUsage, err := convertResponseBetweenProtocols(canonicalProtocolAntigravity, target, body, responseConversionOptions{})
	if err != nil {
		return gatewayBufferedResponse{}, err
	}
	return gatewayBufferedResponse{
		StatusCode:  statusCode,
		ContentType: "application/json",
		Body:        body,
		Usage:       convertedUsage,
	}, nil
}

func (a antigravityProtocolAdapter) ProxyStream(ctx context.Context, w http.ResponseWriter, resp *http.Response, startedAt time.Time, candidate routeengine.Candidate) (streamCaptureState, bool, error) {
	target := a.downstreamProtocol
	if target == "" || target == canonicalProtocolOpenAIImages {
		target = canonicalProtocolOpenAIChat
	}
	return a.proxyStreamAs(ctx, w, resp, startedAt, target, candidate)
}

func (a antigravityProtocolAdapter) projectID(_ gatewayRequest, candidate routeengine.Candidate) string {
	if a.db == nil || candidate.Site.ID == uuid.Nil {
		return ""
	}
	connection, err := store.NewOAuthConnectionRepository(a.db.DB()).GetBySiteID(context.Background(), candidate.Site.ID)
	if err != nil {
		return ""
	}
	meta := map[string]any{}
	if len(connection.Metadata) > 0 {
		_ = json.Unmarshal(connection.Metadata, &meta)
	}
	return stringFromMapAny(meta, "project_id")
}

func encodeCanonicalRequestToAntigravityGemini(request canonicalRequest, upstreamModel string) map[string]any {
	out := map[string]any{
		"model":    upstreamModel,
		"contents": antigravityCanonicalMessagesToContents(request.Messages),
	}
	if system := antigravityCanonicalSystemInstruction(request); len(system) > 0 {
		out["systemInstruction"] = map[string]any{
			"role":  "user",
			"parts": system,
		}
	}
	if generationConfig := antigravityCanonicalGenerationConfig(request); len(generationConfig) > 0 {
		out["generationConfig"] = generationConfig
	}
	if tools := antigravityCanonicalTools(request.Tools); len(tools) > 0 {
		out["tools"] = tools
		out["toolConfig"] = map[string]any{
			"functionCallingConfig": map[string]any{"mode": "AUTO"},
		}
	}
	return out
}

func antigravityCanonicalImagePayload(request canonicalRequest, upstreamModel string, projectID string) map[string]any {
	cleanModel, imageConfig := antigravityCanonicalImageConfig(request, upstreamModel)
	prompt := antigravityCanonicalImagePrompt(request)
	return map[string]any{
		"project":     projectID,
		"requestId":   antigravityRequestID(),
		"model":       cleanModel,
		"userAgent":   "antigravity",
		"requestType": "image_gen",
		"request": map[string]any{
			"contents": []any{map[string]any{
				"role":  "user",
				"parts": []any{map[string]any{"text": prompt}},
			}},
			"generationConfig": map[string]any{
				"candidateCount": 1,
				"imageConfig":    imageConfig,
			},
			"safetySettings": antigravitySafetySettings(),
		},
	}
}

func antigravityCanonicalImagePrompt(request canonicalRequest) string {
	prompt := ""
	params := request.Params
	if request.Image != nil {
		prompt = strings.TrimSpace(request.Image.Prompt)
		if request.Image.Params != nil {
			params = request.Image.Params
		}
	}
	if prompt == "" {
		prompt = "A beautiful image"
	}
	quality := strings.ToLower(strings.TrimSpace(anyString(params["quality"])))
	if quality == "hd" {
		prompt += ", (high quality, highly detailed, 4k resolution, hdr)"
	}
	switch strings.ToLower(strings.TrimSpace(anyString(params["style"]))) {
	case "vivid":
		prompt += ", (vivid colors, dramatic lighting, rich details)"
	case "natural":
		prompt += ", (natural lighting, realistic, photorealistic)"
	}
	return prompt
}

func antigravityCanonicalImageConfig(request canonicalRequest, upstreamModel string) (string, map[string]any) {
	model := strings.ToLower(strings.TrimSpace(upstreamModel))
	if model == "" {
		model = "gemini-3-pro-image"
	}
	params := request.Params
	if request.Image != nil && request.Image.Params != nil {
		params = request.Image.Params
	}
	config := map[string]any{
		"aspectRatio": antigravityImageAspectRatio(params, model),
	}
	if imageSize := antigravityImageSize(params, model); imageSize != "" {
		config["imageSize"] = imageSize
	}
	return antigravityCleanImageModelName(model), config
}

func antigravityImageAspectRatio(payload map[string]any, model string) string {
	size := strings.TrimSpace(firstNonEmptyGatewayString(anyString(payload["aspect_ratio"]), anyString(payload["aspectRatio"]), anyString(payload["size"])))
	if size != "" {
		return antigravityAspectRatioFromSize(size)
	}
	lower := strings.ToLower(model)
	for _, item := range []struct {
		patterns []string
		value    string
	}{
		{[]string{"-21x9", "-21-9"}, "21:9"},
		{[]string{"-16x9", "-16-9"}, "16:9"},
		{[]string{"-9x16", "-9-16"}, "9:16"},
		{[]string{"-4x3", "-4-3"}, "4:3"},
		{[]string{"-3x4", "-3-4"}, "3:4"},
		{[]string{"-3x2", "-3-2"}, "3:2"},
		{[]string{"-2x3", "-2-3"}, "2:3"},
		{[]string{"-5x4", "-5-4"}, "5:4"},
		{[]string{"-4x5", "-4-5"}, "4:5"},
		{[]string{"-1x1", "-1-1"}, "1:1"},
	} {
		for _, pattern := range item.patterns {
			if strings.Contains(lower, pattern) {
				return item.value
			}
		}
	}
	return "1:1"
}

func antigravityImageSize(payload map[string]any, model string) string {
	if direct := strings.TrimSpace(firstNonEmptyGatewayString(anyString(payload["image_size"]), anyString(payload["imageSize"]))); direct != "" {
		return strings.ToUpper(direct)
	}
	switch strings.ToLower(strings.TrimSpace(anyString(payload["quality"]))) {
	case "hd", "4k":
		return "4K"
	case "medium", "2k":
		return "2K"
	case "standard", "1k":
		return "1K"
	}
	lower := strings.ToLower(model)
	switch {
	case strings.Contains(lower, "-4k") || strings.Contains(lower, "-hd"):
		return "4K"
	case strings.Contains(lower, "-2k"):
		return "2K"
	case strings.Contains(lower, "-1k") || strings.Contains(lower, "-standard"):
		return "1K"
	default:
		return ""
	}
}

func antigravityAspectRatioFromSize(size string) string {
	trimmed := strings.TrimSpace(size)
	switch trimmed {
	case "21:9", "16:9", "9:16", "4:3", "3:4", "3:2", "2:3", "5:4", "4:5", "1:1":
		return trimmed
	}
	parts := strings.Split(strings.ToLower(trimmed), "x")
	if len(parts) != 2 {
		return "1:1"
	}
	width, widthErr := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	height, heightErr := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return "1:1"
	}
	ratio := width / height
	for _, item := range []struct {
		value string
		ratio float64
	}{
		{"21:9", 21.0 / 9.0},
		{"16:9", 16.0 / 9.0},
		{"4:3", 4.0 / 3.0},
		{"3:4", 3.0 / 4.0},
		{"9:16", 9.0 / 16.0},
		{"3:2", 3.0 / 2.0},
		{"2:3", 2.0 / 3.0},
		{"5:4", 5.0 / 4.0},
		{"4:5", 4.0 / 5.0},
		{"1:1", 1.0},
	} {
		if math.Abs(ratio-item.ratio) < 0.05 {
			return item.value
		}
	}
	return "1:1"
}

func antigravityCleanImageModelName(model string) string {
	clean := strings.ToLower(strings.TrimSpace(model))
	suffixes := []string{
		"-4k", "-2k", "-1k", "-hd", "-standard", "-medium",
		"-21x9", "-21-9", "-16x9", "-16-9", "-9x16", "-9-16",
		"-4x3", "-4-3", "-3x4", "-3-4", "-3x2", "-3-2",
		"-2x3", "-2-3", "-5x4", "-5-4", "-4x5", "-4-5",
		"-1x1", "-1-1",
	}
	changed := true
	for changed {
		changed = false
		for _, suffix := range suffixes {
			if strings.HasSuffix(clean, suffix) {
				clean = strings.TrimSuffix(clean, suffix)
				changed = true
			}
		}
	}
	return clean
}

func antigravitySafetySettings() []any {
	return []any{
		map[string]any{"category": "HARM_CATEGORY_HARASSMENT", "threshold": "OFF"},
		map[string]any{"category": "HARM_CATEGORY_HATE_SPEECH", "threshold": "OFF"},
		map[string]any{"category": "HARM_CATEGORY_SEXUALLY_EXPLICIT", "threshold": "OFF"},
		map[string]any{"category": "HARM_CATEGORY_DANGEROUS_CONTENT", "threshold": "OFF"},
		map[string]any{"category": "HARM_CATEGORY_CIVIC_INTEGRITY", "threshold": "OFF"},
	}
}

func antigravityCanonicalMessagesToContents(messages []canonicalMessage) []any {
	contents := make([]any, 0, len(messages))
	pendingToolCalls := map[string]canonicalToolCall{}
	for i := 0; i < len(messages); {
		message := messages[i]
		role := strings.TrimSpace(message.Role)
		if role == "system" || role == "developer" {
			i++
			continue
		}
		if message.Type == "function_call" {
			calls := make([]canonicalToolCall, 0, 1)
			for i < len(messages) && messages[i].Type == "function_call" {
				call := canonicalToolCall{
					ID:        strings.TrimSpace(nonEmptyString(messages[i].ToolCallID, messages[i].ID)),
					Type:      "function",
					Name:      messages[i].Name,
					Arguments: messages[i].Arguments,
					Metadata:  messages[i].Metadata,
				}
				calls = append(calls, call)
				i++
			}
			if len(calls) > 0 {
				structuredCalls, degradedParts := antigravitySplitFunctionCallsBySignature(calls)
				parts := append([]any{}, degradedParts...)
				parts = append(parts, antigravityFunctionCallParts(structuredCalls)...)
				contents = append(contents, map[string]any{"role": "model", "parts": parts})
				pendingToolCalls = antigravityPendingToolCalls(structuredCalls)
			}
			continue
		}
		if message.Type == "function_call_output" || role == "tool" {
			outputs := make([]canonicalMessage, 0, 1)
			for i < len(messages) {
				nextRole := strings.TrimSpace(messages[i].Role)
				if messages[i].Type != "function_call_output" && nextRole != "tool" {
					break
				}
				outputs = append(outputs, messages[i])
				i++
			}
			contents = append(contents, map[string]any{
				"role":  "user",
				"parts": antigravityFunctionResponseParts(outputs, pendingToolCalls),
			})
			pendingToolCalls = map[string]canonicalToolCall{}
			continue
		}
		var parts []any
		if len(message.ToolCalls) > 0 && len(message.Content) == 0 {
			parts = nil
		} else {
			parts = antigravityCanonicalContentParts(message.Content, message.RawContent)
		}
		if len(message.ToolCalls) > 0 {
			structuredCalls, degradedParts := antigravitySplitFunctionCallsBySignature(message.ToolCalls)
			parts = append(parts, degradedParts...)
			parts = append(parts, antigravityFunctionCallParts(structuredCalls)...)
		}
		if len(parts) == 0 {
			parts = []any{map[string]any{"text": ""}}
		}
		contents = append(contents, map[string]any{
			"role":  antigravityGeminiRole(role),
			"parts": parts,
		})
		if len(message.ToolCalls) > 0 {
			structuredCalls, _ := antigravitySplitFunctionCallsBySignature(message.ToolCalls)
			pendingToolCalls = antigravityPendingToolCalls(structuredCalls)
		} else {
			pendingToolCalls = map[string]canonicalToolCall{}
		}
		i++
	}
	if len(contents) == 0 {
		contents = append(contents, map[string]any{
			"role":  "user",
			"parts": []any{map[string]any{"text": ""}},
		})
	}
	return contents
}

func antigravitySplitFunctionCallsBySignature(calls []canonicalToolCall) ([]canonicalToolCall, []any) {
	structured := make([]canonicalToolCall, 0, len(calls))
	degraded := make([]any, 0)
	for _, call := range calls {
		if antigravityToolCallThoughtSignature(call.Metadata) != "" {
			structured = append(structured, call)
			continue
		}
		degraded = append(degraded, map[string]any{"text": antigravityToolCallText(call)})
	}
	return structured, degraded
}

func antigravityFunctionCallParts(calls []canonicalToolCall) []any {
	parts := make([]any, 0, len(calls))
	for _, call := range calls {
		functionCall := map[string]any{
			"name": firstNonEmptyGatewayString(call.Name, "tool"),
			"args": unmarshalJSONObjectOrEmpty(call.Arguments),
		}
		if callID := strings.TrimSpace(call.ID); callID != "" {
			functionCall["id"] = callID
		}
		part := map[string]any{"functionCall": functionCall}
		addAntigravityThoughtSignature(part, call.Metadata)
		parts = append(parts, part)
	}
	return parts
}

func antigravityFunctionResponseParts(outputs []canonicalMessage, pending map[string]canonicalToolCall) []any {
	parts := make([]any, 0, len(outputs))
	for _, output := range outputs {
		callID := strings.TrimSpace(output.ToolCallID)
		call, paired := pending[callID]
		if callID != "" && paired {
			functionResponse := map[string]any{
				"id":       callID,
				"name":     firstNonEmptyGatewayString(output.Name, call.Name, callID, "tool"),
				"response": map[string]any{"content": antigravityCanonicalOutputText(output.Output)},
			}
			parts = append(parts, map[string]any{"functionResponse": functionResponse})
			continue
		}
		parts = append(parts, map[string]any{"text": providerToolResultText(callID, output.Output)})
	}
	if len(parts) == 0 {
		return []any{map[string]any{"text": ""}}
	}
	return parts
}

func antigravityPendingToolCalls(calls []canonicalToolCall) map[string]canonicalToolCall {
	out := make(map[string]canonicalToolCall, len(calls))
	for _, call := range calls {
		callID := strings.TrimSpace(call.ID)
		if callID != "" {
			out[callID] = call
		}
	}
	return out
}

func addAntigravityThoughtSignature(part map[string]any, metadata map[string]any) {
	signature := antigravityToolCallThoughtSignature(metadata)
	if signature != "" {
		part["thoughtSignature"] = signature
	}
}

func antigravityToolCallThoughtSignature(metadata map[string]any) string {
	return firstNonEmptyGatewayString(
		rawStringFromAny(metadata["thoughtSignature"]),
		rawStringFromAny(metadata["thought_signature"]),
	)
}

func antigravityToolCallText(call canonicalToolCall) string {
	callID := strings.TrimSpace(call.ID)
	prefix := "Tool call"
	if callID != "" {
		prefix += " " + callID
	}
	name := strings.TrimSpace(call.Name)
	if name != "" {
		prefix += " (" + name + ")"
	}
	args := strings.TrimSpace(call.Arguments)
	if args == "" {
		args = "{}"
	}
	return prefix + ":\n" + args
}

func antigravityCanonicalSystemInstruction(request canonicalRequest) []any {
	parts := []any{}
	if strings.TrimSpace(request.Instructions) != "" {
		parts = append(parts, map[string]any{"text": strings.TrimSpace(request.Instructions)})
	}
	for _, message := range request.Messages {
		role := strings.TrimSpace(message.Role)
		if role != "system" && role != "developer" {
			continue
		}
		text := antigravityCanonicalContentText(message.Content, message.RawContent)
		if strings.TrimSpace(text) != "" {
			parts = append(parts, map[string]any{"text": text})
		}
	}
	return parts
}

func antigravityCanonicalContentParts(parts []canonicalContentPart, raw any) []any {
	if len(parts) == 0 {
		if text := antigravityCanonicalOutputText(raw); text != "" {
			return []any{map[string]any{"text": text}}
		}
		return nil
	}
	out := []any{}
	for _, part := range parts {
		switch part.Type {
		case "input_image":
			if imagePart := antigravityImagePartFromCanonical(part); imagePart != nil {
				out = append(out, imagePart)
			}
		case "input_file":
			if filePart := antigravityInlineDataPart(part.FileData, part.MimeType); filePart != nil {
				out = append(out, filePart)
			}
		default:
			if strings.TrimSpace(part.Text) != "" {
				out = append(out, map[string]any{"text": part.Text})
			}
		}
	}
	return out
}

func antigravityImagePartFromCanonical(part canonicalContentPart) map[string]any {
	imageURL := strings.TrimSpace(part.ImageURL)
	if imageURL == "" && part.Raw != nil {
		imageURL = normalizeImageURLString(part.Raw)
	}
	return antigravityInlineDataPart(imageURL, part.MimeType)
}

func antigravityInlineDataPart(dataURL string, fallbackMimeType string) map[string]any {
	if !strings.HasPrefix(dataURL, "data:") {
		return nil
	}
	comma := strings.Index(dataURL, ",")
	semi := strings.Index(dataURL, ";")
	if comma <= 0 || semi < 5 || semi > comma {
		return nil
	}
	mimeType := strings.TrimSpace(dataURL[5:semi])
	if mimeType == "" {
		mimeType = nonEmptyString(strings.TrimSpace(fallbackMimeType), "application/octet-stream")
	}
	return map[string]any{
		"inlineData": map[string]any{
			"mimeType": mimeType,
			"data":     dataURL[comma+1:],
		},
	}
}

func antigravityCanonicalContentText(parts []canonicalContentPart, raw any) string {
	if len(parts) == 0 {
		return antigravityCanonicalOutputText(raw)
	}
	var builder strings.Builder
	for _, part := range parts {
		if strings.TrimSpace(part.Text) == "" {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString(part.Text)
	}
	return builder.String()
}

func antigravityCanonicalOutputText(raw any) string {
	switch value := raw.(type) {
	case string:
		return value
	case nil:
		return ""
	default:
		data, _ := json.Marshal(value)
		return string(data)
	}
}

func antigravityGeminiRole(role string) string {
	switch role {
	case "assistant":
		return "model"
	default:
		return "user"
	}
}

func antigravityCanonicalGenerationConfig(request canonicalRequest) map[string]any {
	config := map[string]any{}
	if value, ok := request.Params["temperature"]; ok {
		config["temperature"] = value
	}
	if value, ok := request.Params["top_p"]; ok {
		config["topP"] = value
	}
	if value, ok := request.Params["top_k"]; ok {
		config["topK"] = value
	}
	if value, ok := request.Params["max_tokens"]; ok {
		config["maxOutputTokens"] = value
	}
	if value, ok := request.Params["max_completion_tokens"]; ok {
		config["maxOutputTokens"] = value
	}
	if value, ok := request.Params["max_output_tokens"]; ok {
		config["maxOutputTokens"] = value
	}
	if stop, ok := request.Params["stop"]; ok {
		config["stopSequences"] = stop
	}
	if responseFormat, ok := request.Params["response_format"].(map[string]any); ok {
		switch stringFromMapAny(responseFormat, "type") {
		case "json_object":
			config["responseMimeType"] = "application/json"
		}
	}
	return config
}

func antigravityCanonicalTools(tools []canonicalTool) []any {
	functions := []any{}
	for _, tool := range tools {
		if tool.Type != "function" {
			continue
		}
		decl := map[string]any{
			"name":        tool.Name,
			"description": tool.Description,
		}
		if tool.Parameters != nil {
			decl["parameters"] = antigravitySanitizeSchema(tool.Parameters)
		}
		functions = append(functions, decl)
	}
	if len(functions) == 0 {
		return nil
	}
	return []any{map[string]any{"functionDeclarations": functions}}
}

func antigravitySanitizeSchema(raw any) any {
	return antigravitySanitizeSchemaValue(raw, "")
}

func antigravitySanitizeSchemaValue(raw any, parentKey string) any {
	switch value := raw.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, item := range value {
			if !antigravitySchemaKeyAllowed(key) {
				continue
			}
			switch key {
			case "properties":
				props, _ := item.(map[string]any)
				cleanProps := map[string]any{}
				for propName, propSchema := range props {
					cleanProps[propName] = antigravitySanitizeSchemaValue(propSchema, propName)
				}
				out[key] = cleanProps
			case "items":
				out[key] = antigravitySanitizeSchemaValue(item, key)
			case "type":
				if schemaType := antigravitySchemaType(item); schemaType != "" {
					out[key] = schemaType
				}
			default:
				out[key] = antigravitySanitizeSchemaValue(item, key)
			}
		}
		return out
	case []any:
		items := make([]any, 0, len(value))
		for _, item := range value {
			items = append(items, antigravitySanitizeSchemaValue(item, parentKey))
		}
		return items
	default:
		return raw
	}
}

func antigravitySchemaKeyAllowed(key string) bool {
	switch key {
	case "type",
		"format",
		"title",
		"description",
		"nullable",
		"enum",
		"properties",
		"required",
		"items",
		"minimum",
		"maximum",
		"minLength",
		"maxLength",
		"minItems",
		"maxItems",
		"propertyOrdering":
		return true
	default:
		return false
	}
}

func antigravitySchemaType(raw any) string {
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case []any:
		for _, item := range value {
			text := strings.TrimSpace(anyString(item))
			if text != "" && text != "null" {
				return text
			}
		}
	}
	return ""
}

func antigravityUnwrapGeminiBody(body []byte) (map[string]any, error) {
	root := map[string]any{}
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("decode antigravity response: %w", err)
	}
	if response, ok := root["response"].(map[string]any); ok {
		return response, nil
	}
	return root, nil
}

func canonicalResponseFromAntigravityGemini(gemini map[string]any) canonicalResponse {
	text, toolCalls := antigravityGeminiOutput(gemini)
	response := canonicalResponse{
		ID:           "resp_" + uuid.NewString(),
		Model:        stringFromMapAny(gemini, "model"),
		CreatedAt:    time.Now().Unix(),
		FinishReason: antigravityFinishReason(gemini),
		Usage:        antigravityUsage(gemini),
	}
	if text != "" || len(toolCalls) == 0 {
		response.Output = append(response.Output, canonicalOutputItem{
			ID:      "msg_" + uuid.NewString(),
			Type:    "message",
			Status:  "completed",
			Role:    "assistant",
			Text:    text,
			Content: []canonicalContentPart{{Type: "output_text", Text: text}},
		})
	}
	for _, toolCall := range toolCalls {
		fn, _ := toolCall["function"].(map[string]any)
		response.Output = append(response.Output, canonicalOutputItem{
			ID:        stringFromMapAny(toolCall, "id"),
			Type:      "function_call",
			Status:    "completed",
			CallID:    stringFromMapAny(toolCall, "id"),
			Name:      stringFromMapAny(fn, "name"),
			Arguments: anyString(fn["arguments"]),
			Metadata:  toolCallMetadataFromGatewayMap(toolCall),
		})
	}
	return response
}

func antigravityGeminiImages(gemini map[string]any) []map[string]any {
	candidates, _ := gemini["candidates"].([]any)
	images := make([]map[string]any, 0)
	for _, rawCandidate := range candidates {
		candidate, _ := rawCandidate.(map[string]any)
		content, _ := candidate["content"].(map[string]any)
		parts, _ := content["parts"].([]any)
		for _, rawPart := range parts {
			part, _ := rawPart.(map[string]any)
			inlineData, _ := part["inlineData"].(map[string]any)
			if len(inlineData) == 0 {
				inlineData, _ = part["inline_data"].(map[string]any)
			}
			if len(inlineData) > 0 {
				images = append(images, inlineData)
			}
		}
	}
	return images
}

func antigravityGeminiOutput(gemini map[string]any) (string, []map[string]any) {
	candidates, _ := gemini["candidates"].([]any)
	if len(candidates) == 0 {
		return "", nil
	}
	candidate, _ := candidates[0].(map[string]any)
	content, _ := candidate["content"].(map[string]any)
	parts, _ := content["parts"].([]any)
	var text strings.Builder
	toolCalls := []map[string]any{}
	for _, item := range parts {
		part, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if value := stringFromMapAny(part, "text"); value != "" {
			text.WriteString(value)
		}
		if call, ok := part["functionCall"].(map[string]any); ok {
			name := stringFromMapAny(call, "name")
			args := call["args"]
			if args == nil {
				args = map[string]any{}
			}
			argsJSON, _ := json.Marshal(args)
			toolCall := map[string]any{
				"id":   firstNonEmptyGatewayString(stringFromMapAny(call, "id"), "call_"+uuid.NewString()),
				"type": "function",
				"function": map[string]any{
					"name":      name,
					"arguments": string(argsJSON),
				},
			}
			if metadata := toolCallMetadataFromGatewayMap(part); len(metadata) > 0 {
				toolCall["thought_signature"] = metadata["thought_signature"]
				toolCall["thoughtSignature"] = metadata["thoughtSignature"]
			}
			toolCalls = append(toolCalls, toolCall)
		}
	}
	if len(toolCalls) == 0 {
		if parsedToolCalls := antigravityToolCallsFromText(text.String()); len(parsedToolCalls) > 0 {
			return "", parsedToolCalls
		}
	}
	return text.String(), toolCalls
}

func antigravityToolCallsFromText(text string) []map[string]any {
	cleaned := strings.TrimSpace(text)
	if cleaned == "" {
		return nil
	}
	cleaned = strings.TrimSpace(strings.TrimPrefix(cleaned, "\ufeff"))
	if strings.HasPrefix(cleaned, "```") {
		lines := strings.Split(cleaned, "\n")
		if len(lines) >= 3 {
			cleaned = strings.Join(lines[1:len(lines)-1], "\n")
			cleaned = strings.TrimSpace(cleaned)
		}
	}
	var raw any
	if err := json.Unmarshal([]byte(cleaned), &raw); err != nil {
		return nil
	}
	switch value := raw.(type) {
	case []any:
		toolCalls := make([]map[string]any, 0, len(value))
		for _, item := range value {
			call := antigravityToolCallFromAnthropicToolUse(item)
			if call == nil {
				return nil
			}
			toolCalls = append(toolCalls, call)
		}
		return toolCalls
	default:
		if call := antigravityToolCallFromAnthropicToolUse(value); call != nil {
			return []map[string]any{call}
		}
	}
	return nil
}

func antigravityToolCallFromAnthropicToolUse(raw any) map[string]any {
	item, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	if strings.TrimSpace(anyString(item["type"])) != "tool_use" {
		return nil
	}
	name := strings.TrimSpace(anyString(item["name"]))
	if name == "" {
		return nil
	}
	callID := strings.TrimSpace(anyString(item["id"]))
	if callID == "" {
		callID = "call_" + uuid.NewString()
	}
	args := item["input"]
	if args == nil {
		args = map[string]any{}
	}
	argsJSON, _ := json.Marshal(args)
	return map[string]any{
		"id":   callID,
		"type": "function",
		"function": map[string]any{
			"name":      name,
			"arguments": string(argsJSON),
		},
	}
}

func antigravityUsage(gemini map[string]any) gatewayUsage {
	usage, _ := gemini["usageMetadata"].(map[string]any)
	return gatewayUsage{
		PromptTokens:       intFromAnyGateway(usage["promptTokenCount"]),
		CompletionTokens:   intFromAnyGateway(usage["candidatesTokenCount"]),
		TotalTokens:        intFromAnyGateway(usage["totalTokenCount"]),
		CachedPromptTokens: intFromAnyGateway(usage["cachedContentTokenCount"]),
	}.normalized()
}

func antigravityFinishReason(gemini map[string]any) string {
	raw := antigravityRawFinishReason(gemini)
	if raw == "" {
		return "stop"
	}
	switch strings.ToUpper(raw) {
	case "MAX_TOKENS":
		return "length"
	case "SAFETY", "BLOCKLIST", "PROHIBITED_CONTENT":
		return "content_filter"
	case "TOOL_CODE", "MALFORMED_FUNCTION_CALL":
		return "tool_calls"
	default:
		return "stop"
	}
}

func antigravityRawFinishReason(gemini map[string]any) string {
	candidates, _ := gemini["candidates"].([]any)
	if len(candidates) == 0 {
		return ""
	}
	candidate, _ := candidates[0].(map[string]any)
	return stringFromMapAny(candidate, "finishReason")
}

func (a antigravityProtocolAdapter) proxyStreamAs(ctx context.Context, w http.ResponseWriter, resp *http.Response, startedAt time.Time, target canonicalProtocol, candidate routeengine.Candidate) (streamCaptureState, bool, error) {
	return proxyCanonicalStream(ctx, w, resp, startedAt, canonicalProtocolAntigravity, target, canonicalStreamOptions{Candidate: candidate})
}

func antigravityRequestID() string {
	var raw [4]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("agent/%d/%s", time.Now().UnixMilli(), uuid.NewString()[:8])
	}
	return fmt.Sprintf("agent/%d/%s", time.Now().UnixMilli(), hex.EncodeToString(raw[:]))
}

func firstNonEmptyGatewayString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func intFromAnyGateway(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}
