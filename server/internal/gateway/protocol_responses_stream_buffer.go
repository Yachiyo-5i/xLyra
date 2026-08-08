package gateway

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type responsesStreamBufferEvent struct {
	Type              string               `json:"type"`
	Delta             string               `json:"delta"`
	Response          responsesResponse    `json:"response"`
	Error             *responsesAPIError   `json:"error"`
	ItemID            string               `json:"item_id"`
	OutputIndex       int                  `json:"output_index"`
	ContentIndex      int                  `json:"content_index"`
	PartialImageIndex int                  `json:"partial_image_index"`
	Item              *responsesOutputItem `json:"item"`
	Background        string               `json:"background"`
	OutputFormat      string               `json:"output_format"`
	PartialImageB64   string               `json:"partial_image_b64"`
	Quality           string               `json:"quality"`
	RevisedPrompt     string               `json:"revised_prompt"`
	Size              string               `json:"size"`
}

type responsesAPIError struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Param   string `json:"param"`
}

func readBufferedResponsesStreamBody(reader io.Reader) ([]byte, error) {
	if reader == nil {
		return nil, fmt.Errorf("upstream stream body is not available")
	}

	buffered := bufio.NewReaderSize(reader, 64*1024)
	var lastResponse *responsesResponse
	lastEventType := ""
	partialImagesByItemID := make(map[string]responsesOutputItem)
	partialOrder := make([]string, 0)
	textByItemID := make(map[string]*strings.Builder)
	textOrder := make([]string, 0)

	for {
		line, err := buffered.ReadBytes('\n')
		if len(line) > 0 {
			text := strings.TrimSpace(string(line))
			if strings.HasPrefix(text, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(text, "data:"))
				if data != "" && data != "[DONE]" {
					if strings.Contains(data, `"type":"keepalive"`) ||
						strings.Contains(data, `"type":"response.image_generation_call.in_progress"`) ||
						strings.Contains(data, `"type":"response.image_generation_call.generating"`) {
						// Ignore bulky progress events; the final image payload arrives in response.completed.
					} else {
						var event responsesStreamBufferEvent
						if unmarshalErr := json.Unmarshal([]byte(data), &event); unmarshalErr != nil {
							return nil, unmarshalErr
						}
						if failure, ok := semanticFailureFromJSON([]byte(data)); ok {
							return nil, failure
						}
						lastEventType = strings.TrimSpace(event.Type)
						if event.Error != nil {
							return nil, fmt.Errorf("upstream %s: %s", nonEmptyString(event.Error.Code, event.Error.Type, "error"), nonEmptyString(event.Error.Message, "upstream returned an error event"))
						}
						recordTextDeltaEvent(event, textByItemID, &textOrder)
						recordPartialImageEvent(event, partialImagesByItemID, &partialOrder)
						if event.Response.ID != "" || len(event.Response.Output) > 0 || event.Response.Usage != nil {
							copied := event.Response
							mergeTextDeltaOutputs(&copied, textByItemID, textOrder)
							mergePartialImageOutputs(&copied, partialImagesByItemID, partialOrder)
							lastResponse = &copied
						}
						if event.Type == "response.completed" {
							completed := event.Response
							mergeTextDeltaOutputs(&completed, textByItemID, textOrder)
							mergePartialImageOutputs(&completed, partialImagesByItemID, partialOrder)
							encoded, marshalErr := json.Marshal(completed)
							if marshalErr != nil {
								return nil, marshalErr
							}
							return encoded, nil
						}
					}
				}
			}
		}

		if err == nil {
			continue
		}
		if err == io.EOF {
			if lastResponse == nil {
				return nil, fmt.Errorf("upstream stream ended before any response event was received")
			}
			mergeTextDeltaOutputs(lastResponse, textByItemID, textOrder)
			mergePartialImageOutputs(lastResponse, partialImagesByItemID, partialOrder)
			if len(lastResponse.Output) == 0 {
				return nil, fmt.Errorf("upstream stream ended without output (last event: %s)", nonEmptyString(lastEventType, "unknown"))
			}
			encoded, marshalErr := json.Marshal(lastResponse)
			if marshalErr != nil {
				return nil, marshalErr
			}
			return encoded, nil
		}
		return nil, err
	}
}

func recordTextDeltaEvent(event responsesStreamBufferEvent, textByItemID map[string]*strings.Builder, textOrder *[]string) {
	if textByItemID == nil || textOrder == nil {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(event.Type), "response.output_text.delta") {
		return
	}
	delta := event.Delta
	if delta == "" {
		return
	}
	itemID := strings.TrimSpace(event.ItemID)
	if itemID == "" {
		itemID = fmt.Sprintf("output-%d", event.OutputIndex)
	}
	if _, exists := textByItemID[itemID]; !exists {
		textByItemID[itemID] = &strings.Builder{}
		*textOrder = append(*textOrder, itemID)
	}
	textByItemID[itemID].WriteString(delta)
}

func mergeTextDeltaOutputs(response *responsesResponse, textByItemID map[string]*strings.Builder, order []string) {
	if response == nil || len(textByItemID) == 0 {
		return
	}
	indexByID := make(map[string]int, len(response.Output))
	for i, item := range response.Output {
		if strings.TrimSpace(item.ID) != "" {
			indexByID[item.ID] = i
		}
	}
	for _, itemID := range order {
		builder := textByItemID[itemID]
		if builder == nil || builder.Len() == 0 {
			continue
		}
		text := builder.String()
		if index, ok := indexByID[itemID]; ok {
			mergeTextIntoResponseOutputItem(&response.Output[index], text)
			continue
		}
		response.Output = append(response.Output, responsesOutputItem{
			ID:     itemID,
			Type:   "message",
			Role:   "assistant",
			Status: "completed",
			Content: []responsesOutputContent{
				{Type: "output_text", Text: text},
			},
		})
	}
}

func mergeTextIntoResponseOutputItem(item *responsesOutputItem, text string) {
	if item == nil || strings.TrimSpace(text) == "" {
		return
	}
	item.Type = nonEmptyString(item.Type, "message")
	item.Role = nonEmptyString(item.Role, "assistant")
	item.Status = nonEmptyString(item.Status, "completed")
	for i, content := range item.Content {
		if content.Type == "output_text" {
			if content.Text == "" {
				item.Content[i].Text = text
			}
			return
		}
	}
	item.Content = append(item.Content, responsesOutputContent{Type: "output_text", Text: text})
}

func recordPartialImageEvent(event responsesStreamBufferEvent, partialImagesByItemID map[string]responsesOutputItem, partialOrder *[]string) {
	if partialImagesByItemID == nil || partialOrder == nil {
		return
	}
	itemID := strings.TrimSpace(event.ItemID)
	if event.Item != nil {
		itemID = nonEmptyString(itemID, event.Item.ID)
		if strings.EqualFold(strings.TrimSpace(event.Item.Type), "image_generation_call") {
			stored := upsertPartialImageOutput(partialImagesByItemID, partialOrder, itemID)
			mergeResponseOutputItem(&stored, *event.Item)
			partialImagesByItemID[stored.ID] = stored
			return
		}
	}
	if !strings.EqualFold(strings.TrimSpace(event.Type), "response.image_generation_call.partial_image") {
		return
	}
	stored := upsertPartialImageOutput(partialImagesByItemID, partialOrder, itemID)
	mergeResponseOutputItem(&stored, responsesOutputItem{
		ID:            nonEmptyString(itemID, stored.ID),
		Type:          "image_generation_call",
		Status:        "completed",
		Result:        strings.TrimSpace(event.PartialImageB64),
		RevisedPrompt: strings.TrimSpace(event.RevisedPrompt),
		Background:    strings.TrimSpace(event.Background),
		OutputFormat:  strings.TrimSpace(event.OutputFormat),
		Quality:       strings.TrimSpace(event.Quality),
		Size:          strings.TrimSpace(event.Size),
	})
	partialImagesByItemID[stored.ID] = stored
}

func upsertPartialImageOutput(partialImagesByItemID map[string]responsesOutputItem, partialOrder *[]string, itemID string) responsesOutputItem {
	key := strings.TrimSpace(itemID)
	if key == "" {
		key = fmt.Sprintf("partial-image-%d", len(*partialOrder)+1)
	}
	if _, exists := partialImagesByItemID[key]; !exists {
		partialImagesByItemID[key] = responsesOutputItem{
			ID:     key,
			Type:   "image_generation_call",
			Status: "completed",
		}
		*partialOrder = append(*partialOrder, key)
	}
	item := partialImagesByItemID[key]
	if strings.TrimSpace(item.ID) == "" {
		item.ID = key
	}
	if strings.TrimSpace(item.Type) == "" {
		item.Type = "image_generation_call"
	}
	if strings.TrimSpace(item.Status) == "" {
		item.Status = "completed"
	}
	partialImagesByItemID[key] = item
	return item
}

func mergePartialImageOutputs(response *responsesResponse, partials map[string]responsesOutputItem, order []string) {
	if response == nil || len(response.Output) > 0 || len(partials) == 0 {
		return
	}
	response.Output = make([]responsesOutputItem, 0, len(order))
	for _, itemID := range order {
		item, ok := partials[itemID]
		if !ok || strings.TrimSpace(item.Result) == "" {
			continue
		}
		response.Output = append(response.Output, item)
	}
}

func mergeResponseOutputItem(target *responsesOutputItem, source responsesOutputItem) {
	if target == nil {
		return
	}
	target.ID = nonEmptyString(target.ID, source.ID)
	target.Type = nonEmptyString(target.Type, source.Type)
	target.Role = nonEmptyString(target.Role, source.Role)
	target.Status = nonEmptyString(target.Status, source.Status)
	if len(target.Content) == 0 && len(source.Content) > 0 {
		target.Content = source.Content
	}
	target.CallID = nonEmptyString(target.CallID, source.CallID)
	target.Name = nonEmptyString(target.Name, source.Name)
	target.Arguments = nonEmptyString(target.Arguments, source.Arguments)
	target.Result = nonEmptyString(target.Result, source.Result)
	target.RevisedPrompt = nonEmptyString(target.RevisedPrompt, source.RevisedPrompt)
	target.Background = nonEmptyString(target.Background, source.Background)
	target.OutputFormat = nonEmptyString(target.OutputFormat, source.OutputFormat)
	target.Quality = nonEmptyString(target.Quality, source.Quality)
	target.Size = nonEmptyString(target.Size, source.Size)
}

func shouldBufferAsResponsesStream(contentType string, reader *bufio.Reader) bool {
	if strings.Contains(strings.ToLower(strings.TrimSpace(contentType)), "text/event-stream") {
		return true
	}
	if reader == nil {
		return false
	}
	peeked, err := reader.Peek(16)
	if err != nil && err != io.EOF && err != bufio.ErrBufferFull {
		return false
	}
	return looksLikeSSEBody(peeked)
}

func nonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
