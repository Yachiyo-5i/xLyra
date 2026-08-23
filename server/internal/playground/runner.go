package playground

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/gateway"
	"xlyra/server/internal/store"
)

type captureWriter struct {
	header http.Header
	status int
	mu     sync.Mutex
	write  func([]byte) error
}

func newCaptureWriter(write func([]byte) error) *captureWriter {
	return &captureWriter{header: make(http.Header), status: http.StatusOK, write: write}
}

func (w *captureWriter) Header() http.Header {
	return w.header
}

func (w *captureWriter) WriteHeader(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.status = status
}

func (w *captureWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.write != nil {
		if err := w.write(data); err != nil {
			return 0, err
		}
	}
	return len(data), nil
}

func (w *captureWriter) Flush() {}

type sseDecoder struct {
	buffer string
	onData func(string) error
}

func (d *sseDecoder) Write(data []byte) error {
	d.buffer += string(data)
	for {
		index, size := sseBoundary(d.buffer)
		if index < 0 {
			return nil
		}
		raw := d.buffer[:index]
		d.buffer = d.buffer[index+size:]
		lines := strings.FieldsFunc(raw, func(r rune) bool { return r == '\r' || r == '\n' })
		values := make([]string, 0)
		for _, line := range lines {
			line = strings.TrimLeft(line, " \t")
			if strings.HasPrefix(line, "data:") {
				values = append(values, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
		}
		if len(values) > 0 {
			value := strings.Join(values, "\n")
			if value != "[DONE]" {
				if err := d.onData(value); err != nil {
					return err
				}
			}
		}
	}
}

func sseBoundary(value string) (int, int) {
	best := -1
	size := 0
	for _, boundary := range []string{"\r\n\r\n", "\n\n", "\r\r"} {
		if index := strings.Index(value, boundary); index >= 0 && (best < 0 || index < best) {
			best = index
			size = len(boundary)
		}
	}
	return best, size
}

type chatRunState struct {
	content       string
	reasoning     string
	pendingText   string
	pendingReason string
	usage         *Usage
	siteName      string
	failure       string
	lastFlush     time.Time
}

const messagesMaxTokens = 16384

func (s *Service) execute(run store.PlaygroundRun, apiKey store.APIKey, conversation store.PlaygroundConversation, payload RunPayload, ready chan<- struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.mu.Lock()
	s.cancels[run.ID] = cancel
	s.finishes[run.ID] = done
	s.mu.Unlock()
	close(ready)
	defer func() {
		cancel()
		s.mu.Lock()
		delete(s.cancels, run.ID)
		delete(s.finishes, run.ID)
		s.mu.Unlock()
		close(done)
	}()
	if current, err := s.repo.GetRun(context.Background(), run.ID); err == nil && current.CancelRequested {
		cancel()
	}
	now := time.Now()
	_ = s.patchRun(ctx, run.ConversationID, run.ID, map[string]any{"status": "running", "started_at": now, "updated_at": now})
	if payload.Mode == ModeImage {
		s.executeImage(ctx, run, apiKey, &conversation, payload)
		return
	}
	s.executeChat(ctx, run, apiKey, &conversation, payload)
}

func (s *Service) executeChat(ctx context.Context, run store.PlaygroundRun, apiKey store.APIKey, conversation *store.PlaygroundConversation, payload RunPayload) {
	startedAt := time.Now()
	state := &chatRunState{lastFlush: startedAt}
	body, err := s.chatGatewayBody(ctx, payload)
	if err != nil {
		s.finishFailedRun(run, conversation, payload, state, startedAt, err, false)
		return
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		s.finishFailedRun(run, conversation, payload, state, startedAt, err, false)
		return
	}
	decoder := &sseDecoder{onData: func(value string) error {
		if err := s.consumeChatEvent(payload.Protocol, value, state); err != nil {
			return err
		}
		if len(state.pendingText)+len(state.pendingReason) >= 4096 || time.Since(state.lastFlush) >= 300*time.Millisecond {
			return s.flushChatDelta(ctx, run, conversation, payload, state)
		}
		return nil
	}}
	writer := newCaptureWriter(decoder.Write)
	request, err := http.NewRequestWithContext(auth.WithAPIKey(ctx, apiKey), http.MethodPost, gatewayPath(payload.Protocol), bytes.NewReader(encoded))
	if err != nil {
		s.finishFailedRun(run, conversation, payload, state, startedAt, err, false)
		return
	}
	request.Header.Set("Content-Type", "application/json")
	s.serveGateway(writer, request, payload.Protocol, false)
	state.siteName = routeSiteName(writer.Header())
	if err := s.flushChatDelta(context.Background(), run, conversation, payload, state); err != nil && state.failure == "" {
		state.failure = err.Error()
	}
	if ctx.Err() != nil {
		s.finishFailedRun(run, conversation, payload, state, startedAt, context.Canceled, true)
		return
	}
	if writer.status < 200 || writer.status >= 300 {
		if state.failure == "" {
			state.failure = fmt.Sprintf("gateway returned status %d", writer.status)
		}
	}
	if state.failure != "" {
		s.finishFailedRun(run, conversation, payload, state, startedAt, errors.New(state.failure), false)
		return
	}
	duration := time.Since(startedAt).Milliseconds()
	message := ChatMessage{ID: payload.MessageID, Role: "assistant", Content: state.content, Reasoning: state.reasoning, Usage: state.usage, Model: payload.Model, SiteName: state.siteName, ResponseDurationMS: &duration, CreatedAt: startedAt.UnixMilli()}
	result, err := s.append(context.Background(), conversation, "assistant_final", run.ID, message, conversation.Title)
	if err == nil {
		result, err = s.append(context.Background(), conversation, "turn_completed", run.ID, map[string]any{"response_duration_ms": duration}, conversation.Title)
	}
	if err != nil {
		s.finishFailedRun(run, conversation, payload, state, startedAt, err, false)
		return
	}
	now := time.Now()
	_ = s.patchRun(context.Background(), run.ConversationID, run.ID, map[string]any{"status": "completed", "completed_at": now, "updated_at": now})
	_ = s.repo.UpdateTurnIndex(context.Background(), run.ID, "completed", result.Ordinal, result.Offset)
}

func (s *Service) flushChatDelta(ctx context.Context, run store.PlaygroundRun, conversation *store.PlaygroundConversation, payload RunPayload, state *chatRunState) error {
	if state.pendingText == "" && state.pendingReason == "" {
		return nil
	}
	delta := deltaPayload{MessageID: payload.MessageID, Content: state.pendingText, Reasoning: state.pendingReason}
	if _, err := s.append(ctx, conversation, "assistant_delta", run.ID, delta, conversation.Title); err != nil {
		return err
	}
	state.pendingText = ""
	state.pendingReason = ""
	state.lastFlush = time.Now()
	return nil
}

func (s *Service) consumeChatEvent(protocol string, value string, state *chatRunState) error {
	var event map[string]any
	if err := json.Unmarshal([]byte(value), &event); err != nil {
		return nil
	}
	if protocol == "responses" {
		eventType, _ := event["type"].(string)
		switch eventType {
		case "response.output_text.delta":
			state.addContent(stringValue(event["delta"]))
		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
			state.addReasoning(stringValue(event["delta"]))
		case "response.completed", "response.incomplete":
			if response, ok := event["response"].(map[string]any); ok {
				state.usage = responsesUsage(response["usage"])
			}
		case "response.failed", "error":
			state.failure = nestedError(event)
		}
		return nil
	}
	if protocol == "messages" {
		eventType, _ := event["type"].(string)
		switch eventType {
		case "content_block_delta":
			if delta, ok := event["delta"].(map[string]any); ok {
				switch stringValue(delta["type"]) {
				case "text_delta":
					state.addContent(stringValue(delta["text"]))
				case "thinking_delta":
					state.addReasoning(stringValue(delta["thinking"]))
				}
			}
		case "message_start":
			if message, ok := event["message"].(map[string]any); ok {
				state.usage = anthropicUsage(message["usage"], state.usage)
			}
		case "message_delta":
			state.usage = anthropicUsage(event["usage"], state.usage)
		case "error":
			state.failure = nestedError(event)
		}
		return nil
	}
	if errorValue, ok := event["error"].(map[string]any); ok {
		state.failure = stringValue(errorValue["message"])
	}
	if choices, ok := event["choices"].([]any); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			if delta, ok := choice["delta"].(map[string]any); ok {
				state.addContent(stringValue(delta["content"]))
				state.addReasoning(stringValue(delta["reasoning_content"]))
			}
		}
	}
	if usage, ok := event["usage"].(map[string]any); ok {
		state.usage = openAIUsage(usage)
	}
	return nil
}

func (s *chatRunState) addContent(value string) {
	s.content += value
	s.pendingText += value
}

func (s *chatRunState) addReasoning(value string) {
	s.reasoning += value
	s.pendingReason += value
}

func (s *Service) finishFailedRun(run store.PlaygroundRun, conversation *store.PlaygroundConversation, payload RunPayload, state *chatRunState, startedAt time.Time, failure error, cancelled bool) {
	duration := time.Since(startedAt).Milliseconds()
	status := "failed"
	eventType := "turn_failed"
	message := failure.Error()
	if cancelled {
		status = "cancelled"
		eventType = "turn_cancelled"
		message = "request cancelled"
	} else if state != nil && (state.content != "" || state.reasoning != "") {
		status = "partial"
	}
	if state != nil && (state.content != "" || state.reasoning != "") {
		final := ChatMessage{ID: payload.MessageID, Role: "assistant", Content: state.content, Reasoning: state.reasoning, Usage: state.usage, Model: payload.Model, SiteName: state.siteName, ResponseDurationMS: &duration, CreatedAt: startedAt.UnixMilli()}
		_, _ = s.append(context.Background(), conversation, "assistant_final", run.ID, final, conversation.Title)
	}
	result, _ := s.append(context.Background(), conversation, eventType, run.ID, failurePayload{MessageID: payload.MessageID, EntryID: payload.EntryID, Error: message, ResponseDurationMS: duration}, conversation.Title)
	now := time.Now()
	_ = s.patchRun(context.Background(), run.ConversationID, run.ID, map[string]any{"status": status, "error": message, "completed_at": now, "updated_at": now})
	_ = s.repo.UpdateTurnIndex(context.Background(), run.ID, status, result.Ordinal, result.Offset)
}

func (s *Service) chatGatewayBody(ctx context.Context, payload RunPayload) (map[string]any, error) {
	if payload.Chat == nil {
		return nil, fmt.Errorf("chat payload is missing")
	}
	messages := make([]map[string]any, 0, len(payload.Chat.Messages)+1)
	if strings.TrimSpace(payload.Chat.SystemPrompt) != "" && payload.Protocol != "responses" && payload.Protocol != "messages" {
		messages = append(messages, map[string]any{"role": "system", "content": payload.Chat.SystemPrompt})
	}
	for _, message := range payload.Chat.Messages {
		if message.ID == payload.MessageID || message.Error != "" {
			continue
		}
		content, err := s.gatewayMessageContent(ctx, payload.Protocol, message)
		if err != nil {
			return nil, err
		}
		messages = append(messages, map[string]any{"role": message.Role, "content": content})
	}
	if payload.Protocol == "responses" {
		input := make([]map[string]any, 0, len(messages))
		for _, message := range messages {
			if message["role"] == "system" {
				continue
			}
			input = append(input, map[string]any{"type": "message", "role": message["role"], "content": message["content"]})
		}
		body := map[string]any{"model": payload.Model, "input": input, "stream": true}
		if strings.TrimSpace(payload.Chat.SystemPrompt) != "" {
			body["instructions"] = strings.TrimSpace(payload.Chat.SystemPrompt)
		}
		if payload.ReasoningEffort != "" {
			body["reasoning"] = map[string]any{"effort": payload.ReasoningEffort, "summary": "auto"}
		}
		return body, nil
	}
	if payload.Protocol == "messages" {
		body := map[string]any{"model": payload.Model, "max_tokens": messagesMaxTokens, "messages": messages, "stream": true}
		if strings.TrimSpace(payload.Chat.SystemPrompt) != "" {
			body["system"] = strings.TrimSpace(payload.Chat.SystemPrompt)
		}
		return body, nil
	}
	body := map[string]any{"model": payload.Model, "messages": messages, "stream": true, "stream_options": map[string]any{"include_usage": true}}
	if payload.ReasoningEffort != "" {
		body["reasoning_effort"] = payload.ReasoningEffort
	}
	return body, nil
}

func (s *Service) gatewayMessageContent(ctx context.Context, protocol string, message ChatMessage) (any, error) {
	if len(message.Attachments) == 0 {
		return message.Content, nil
	}
	parts := make([]map[string]any, 0, len(message.Attachments)+1)
	if message.Content != "" {
		partType := "text"
		if protocol == "responses" {
			if message.Role == "assistant" {
				partType = "output_text"
			} else {
				partType = "input_text"
			}
		}
		parts = append(parts, map[string]any{"type": partType, "text": message.Content})
	}
	for _, attachment := range message.Attachments {
		id, err := uuid.Parse(attachment.AssetID)
		if err != nil {
			return nil, fmt.Errorf("invalid attachment asset")
		}
		dataURL, err := s.assets.DataURL(ctx, id)
		if err != nil {
			return nil, err
		}
		if protocol == "responses" {
			if strings.HasPrefix(attachment.MIMEType, "image/") {
				parts = append(parts, map[string]any{"type": "input_image", "image_url": dataURL})
			} else {
				parts = append(parts, map[string]any{"type": "input_file", "filename": attachment.Name, "file_data": dataURL})
			}
			continue
		}
		if protocol == "messages" {
			comma := strings.IndexByte(dataURL, ',')
			data := dataURL
			if comma >= 0 {
				data = dataURL[comma+1:]
			}
			if strings.HasPrefix(attachment.MIMEType, "image/") {
				parts = append(parts, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": attachment.MIMEType, "data": data}})
			} else {
				parts = append(parts, map[string]any{"type": "document", "title": attachment.Name, "source": map[string]any{"type": "base64", "media_type": attachment.MIMEType, "data": data}})
			}
			continue
		}
		if strings.HasPrefix(attachment.MIMEType, "image/") {
			parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": dataURL}})
		} else {
			parts = append(parts, map[string]any{"type": "file", "file": map[string]any{"filename": attachment.Name, "file_data": dataURL}})
		}
	}
	return parts, nil
}

func gatewayPath(protocol string) string {
	switch protocol {
	case "responses":
		return "/responses"
	case "messages":
		return "/messages"
	default:
		return "/chat/completions"
	}
}

func (s *Service) serveGateway(writer http.ResponseWriter, request *http.Request, protocol string, image bool) {
	if image {
		if strings.Contains(request.URL.Path, "edits") {
			s.gateway.ImagesEdits(writer, request)
		} else {
			s.gateway.ImagesGenerations(writer, request)
		}
		return
	}
	switch protocol {
	case "responses":
		s.gateway.Responses(writer, request)
	case "messages":
		s.gateway.Messages(writer, request)
	default:
		s.gateway.ChatCompletions(writer, request)
	}
}

func (s *Service) executeImage(ctx context.Context, run store.PlaygroundRun, apiKey store.APIKey, conversation *store.PlaygroundConversation, payload RunPayload) {
	startedAt := time.Now()
	body, contentType, path, err := s.imageGatewayBody(ctx, payload)
	if err != nil {
		s.finishFailedRun(run, conversation, payload, nil, startedAt, err, false)
		return
	}
	var response bytes.Buffer
	writer := newCaptureWriter(func(data []byte) error {
		if response.Len()+len(data) > maxAssetBytes*2 {
			return fmt.Errorf("image response is too large")
		}
		_, err := response.Write(data)
		return err
	})
	request, err := http.NewRequestWithContext(auth.WithAPIKey(ctx, apiKey), http.MethodPost, path, body)
	if err != nil {
		s.finishFailedRun(run, conversation, payload, nil, startedAt, err, false)
		return
	}
	request.Header.Set("Content-Type", contentType)
	s.serveGateway(writer, request, "", true)
	if ctx.Err() != nil {
		s.finishFailedRun(run, conversation, payload, nil, startedAt, context.Canceled, true)
		return
	}
	if writer.status < 200 || writer.status >= 300 {
		s.finishFailedRun(run, conversation, payload, nil, startedAt, gatewayJSONError(response.Bytes(), writer.status), false)
		return
	}
	var upstream struct {
		Data []struct {
			Base64 string `json:"b64_json"`
			URL    string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Bytes(), &upstream); err != nil {
		s.finishFailedRun(run, conversation, payload, nil, startedAt, fmt.Errorf("decode image response: %w", err), false)
		return
	}
	results := make([]ImageResult, 0, len(upstream.Data))
	saved := make([]store.PlaygroundAsset, 0, len(upstream.Data))
	for _, item := range upstream.Data {
		var asset store.PlaygroundAsset
		if item.Base64 != "" {
			data, decodeErr := base64.StdEncoding.DecodeString(item.Base64)
			if decodeErr != nil {
				err = decodeErr
				break
			}
			asset, err = s.assets.SaveBytes(context.Background(), conversation.ID, run.ID, "generated-images", "generated.png", "image/png", data)
		} else if item.URL != "" {
			asset, err = s.assets.SaveRemote(context.Background(), conversation.ID, run.ID, "generated-images", item.URL)
		} else {
			err = fmt.Errorf("image response item has no image data")
		}
		if err != nil {
			break
		}
		saved = append(saved, asset)
		results = append(results, ImageResult{ID: uuid.NewString(), Src: assetURL(asset.ID), AssetID: asset.ID.String()})
	}
	if err != nil || len(results) == 0 {
		if err == nil {
			err = fmt.Errorf("image response did not contain images")
		}
		s.discardAssets(saved)
		s.finishFailedRun(run, conversation, payload, nil, startedAt, err, false)
		return
	}
	duration := time.Since(startedAt).Milliseconds()
	entry := payload.Image.Entries[len(payload.Image.Entries)-1]
	entry.Images = results
	entry.SiteName = routeSiteName(writer.Header())
	entry.Pending = false
	entry.ResponseDurationMS = &duration
	result, err := s.append(context.Background(), conversation, "image_final", run.ID, entry, conversation.Title)
	if err == nil {
		result, err = s.append(context.Background(), conversation, "turn_completed", run.ID, map[string]any{"response_duration_ms": duration}, conversation.Title)
	}
	if err != nil {
		s.finishFailedRun(run, conversation, payload, nil, startedAt, err, false)
		return
	}
	now := time.Now()
	_ = s.patchRun(context.Background(), run.ConversationID, run.ID, map[string]any{"status": "completed", "completed_at": now, "updated_at": now})
	_ = s.repo.UpdateTurnIndex(context.Background(), run.ID, "completed", result.Ordinal, result.Offset)
}

func (s *Service) discardAssets(assets []store.PlaygroundAsset) {
	for _, asset := range assets {
		_ = s.assets.Delete(context.Background(), asset)
	}
}

func (s *Service) imageGatewayBody(ctx context.Context, payload RunPayload) (io.Reader, string, string, error) {
	if payload.Image == nil || len(payload.Image.Entries) == 0 {
		return nil, "", "", fmt.Errorf("image payload is missing")
	}
	entry := payload.Image.Entries[len(payload.Image.Entries)-1]
	if entry.Mode != "edit" || len(entry.SourceAssetIDs) == 0 {
		body := map[string]any{"model": payload.Model, "prompt": entry.Prompt, "n": 1, "response_format": "b64_json"}
		if entry.Size != "" && entry.Size != "auto" {
			body["size"] = entry.Size
		}
		encoded, err := json.Marshal(body)
		return bytes.NewReader(encoded), "application/json", "/images/generations", err
	}
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	for key, value := range map[string]string{"model": payload.Model, "prompt": entry.Prompt, "n": "1"} {
		if err := writer.WriteField(key, value); err != nil {
			return nil, "", "", err
		}
	}
	if entry.Size != "" && entry.Size != "auto" {
		if err := writer.WriteField("size", entry.Size); err != nil {
			return nil, "", "", err
		}
	}
	for index, value := range entry.SourceAssetIDs {
		id, err := uuid.Parse(value)
		if err != nil {
			return nil, "", "", err
		}
		asset, data, err := s.assets.Read(ctx, id)
		if err != nil {
			return nil, "", "", err
		}
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image"; filename="source-%d%s"`, index+1, extensionForMIME(asset.MIMEType)))
		header.Set("Content-Type", asset.MIMEType)
		part, err := writer.CreatePart(header)
		if err != nil {
			return nil, "", "", err
		}
		if _, err := part.Write(data); err != nil {
			return nil, "", "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", "", err
	}
	return bytes.NewReader(buffer.Bytes()), writer.FormDataContentType(), "/images/edits", nil
}

func routeSiteName(headers http.Header) string {
	value := strings.TrimSpace(headers.Get(gateway.RouteSiteHeader))
	if decoded, err := url.PathUnescape(value); err == nil {
		return decoded
	}
	return value
}

func gatewayJSONError(data []byte, status int) error {
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &payload) == nil && payload.Error.Message != "" {
		return errors.New(payload.Error.Message)
	}
	return fmt.Errorf("gateway returned status %d", status)
}

func stringValue(value any) string {
	result, _ := value.(string)
	return result
}

func int64Value(value any) int64 {
	switch number := value.(type) {
	case float64:
		return int64(number)
	case int64:
		return number
	case json.Number:
		result, _ := number.Int64()
		return result
	default:
		return 0
	}
}

func openAIUsage(value map[string]any) *Usage {
	prompt := int64Value(value["prompt_tokens"])
	completion := int64Value(value["completion_tokens"])
	total := int64Value(value["total_tokens"])
	if total == 0 {
		total = prompt + completion
	}
	return &Usage{PromptTokens: prompt, CompletionTokens: completion, TotalTokens: total}
}

func responsesUsage(value any) *Usage {
	usage, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	prompt := int64Value(usage["input_tokens"])
	completion := int64Value(usage["output_tokens"])
	total := int64Value(usage["total_tokens"])
	if total == 0 {
		total = prompt + completion
	}
	return &Usage{PromptTokens: prompt, CompletionTokens: completion, TotalTokens: total}
}

func anthropicUsage(value any, current *Usage) *Usage {
	usage, ok := value.(map[string]any)
	if !ok {
		return current
	}
	result := Usage{}
	if current != nil {
		result = *current
	}
	if input := int64Value(usage["input_tokens"]); input > 0 {
		result.PromptTokens = input
	}
	if output := int64Value(usage["output_tokens"]); output > 0 {
		result.CompletionTokens = output
	}
	result.TotalTokens = result.PromptTokens + result.CompletionTokens
	return &result
}

func nestedError(event map[string]any) string {
	if value, ok := event["error"].(map[string]any); ok {
		if message := stringValue(value["message"]); message != "" {
			return message
		}
	}
	if response, ok := event["response"].(map[string]any); ok {
		if value, ok := response["error"].(map[string]any); ok {
			if message := stringValue(value["message"]); message != "" {
				return message
			}
		}
	}
	if message := stringValue(event["message"]); message != "" {
		return message
	}
	return "response failed"
}
