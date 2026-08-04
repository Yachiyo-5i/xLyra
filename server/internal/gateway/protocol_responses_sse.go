package gateway

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
)

func parseBufferedResponsesBody(body []byte) ([]byte, bool, error) {
	if !looksLikeSSEBody(body) {
		return body, false, nil
	}

	scanner := bufio.NewScanner(bytes.NewReader(body))
	// Allow larger SSE lines in case image_generation_call results carry long base64 payloads.
	buffer := make([]byte, 0, 64*1024)
	scanner.Buffer(buffer, 32<<20)

	var lastResponse *responsesResponse
	textByItemID := make(map[string]*strings.Builder)
	textOrder := make([]string, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		event := responsesStreamBufferEvent{}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return nil, true, err
		}
		recordTextDeltaEvent(event, textByItemID, &textOrder)
		if event.Response.ID == "" && len(event.Response.Output) == 0 && event.Response.Usage == nil {
			continue
		}
		copied := event.Response
		mergeTextDeltaOutputs(&copied, textByItemID, textOrder)
		lastResponse = &copied
		if event.Type == "response.completed" {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, true, err
	}
	if lastResponse == nil {
		return nil, true, nil
	}
	mergeTextDeltaOutputs(lastResponse, textByItemID, textOrder)
	normalized, err := json.Marshal(lastResponse)
	if err != nil {
		return nil, true, err
	}
	return normalized, true, nil
}

func looksLikeSSEBody(body []byte) bool {
	trimmed := strings.TrimSpace(string(body))
	return strings.HasPrefix(trimmed, "data:") || strings.HasPrefix(trimmed, "event:")
}
