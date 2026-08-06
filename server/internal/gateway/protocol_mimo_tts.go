package gateway

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type mimoTTSChatStreamEvent struct {
	ID      string          `json:"id"`
	Created int64           `json:"created"`
	Model   string          `json:"model"`
	Error   json.RawMessage `json:"error"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role    string `json:"role"`
			Content string `json:"content"`
			Audio   struct {
				Data string `json:"data"`
			} `json:"audio"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *completionUsage `json:"usage"`
}

func isMiMoV25TTSModel(model string) bool {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "mimo-v2.5-tts", "mimo-v2.5-tts-voicedesign", "mimo-v2.5-tts-voiceclone":
		return true
	default:
		return false
	}
}

func bufferMiMoTTSChatStream(body []byte, responseFormat string) ([]byte, completionUsage, error) {
	reader := bufio.NewReader(bytes.NewReader(body))
	var id string
	var model string
	var created int64
	var role string
	var content strings.Builder
	var audio bytes.Buffer
	var finishReason *string
	var usage completionUsage
	hasUsage := false
	receivedChoice := false
	receivedDone := false

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			data, ok := chatSSEData(line)
			if ok && data == "[DONE]" {
				receivedDone = true
			}
			if ok && data != "[DONE]" {
				var event mimoTTSChatStreamEvent
				if unmarshalErr := json.Unmarshal([]byte(data), &event); unmarshalErr != nil {
					return nil, completionUsage{}, unmarshalErr
				}
				if len(event.Error) > 0 && string(event.Error) != "null" {
					return nil, completionUsage{}, fmt.Errorf("upstream stream error: %s", string(event.Error))
				}
				id = nonEmptyString(event.ID, id)
				model = nonEmptyString(event.Model, model)
				if event.Created != 0 {
					created = event.Created
				}
				if event.Usage != nil {
					usage = event.Usage.normalized()
					hasUsage = true
				}
				for _, choice := range event.Choices {
					if choice.Index != 0 {
						continue
					}
					receivedChoice = true
					role = nonEmptyString(choice.Delta.Role, role)
					content.WriteString(choice.Delta.Content)
					if choice.FinishReason != nil {
						finishReason = choice.FinishReason
					}
					decoded, decodeErr := decodeMiMoTTSAudioChunk(choice.Delta.Audio.Data)
					if decodeErr != nil {
						return nil, completionUsage{}, decodeErr
					}
					audio.Write(decoded)
				}
			}
		}
		if err == nil {
			continue
		}
		if err != io.EOF {
			return nil, completionUsage{}, err
		}
		break
	}

	if !receivedChoice {
		return nil, completionUsage{}, fmt.Errorf("upstream stream ended before any completion choice was received")
	}
	if !receivedDone && finishReason == nil {
		return nil, completionUsage{}, fmt.Errorf("upstream stream ended before completion")
	}
	if audio.Len() == 0 {
		return nil, completionUsage{}, fmt.Errorf("upstream stream ended without audio data")
	}
	message := map[string]any{"role": nonEmptyString(role, "assistant"), "content": nil}
	if content.Len() > 0 {
		message["content"] = content.String()
	}
	audioData := audio.Bytes()
	if strings.EqualFold(strings.TrimSpace(responseFormat), "wav") {
		audioData = mimoTTSWAV(audioData)
	}
	message["audio"] = map[string]any{"data": base64.StdEncoding.EncodeToString(audioData)}
	response := map[string]any{
		"id":      id,
		"object":  "chat.completion",
		"created": created,
		"model":   model,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       message,
			"finish_reason": finishReason,
		}},
	}
	if hasUsage {
		response["usage"] = usage
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return nil, completionUsage{}, err
	}
	return encoded, usage, nil
}

func mimoTTSWAV(pcm []byte) []byte {
	result := make([]byte, 44+len(pcm))
	copy(result[0:4], "RIFF")
	binary.LittleEndian.PutUint32(result[4:8], uint32(36+len(pcm)))
	copy(result[8:12], "WAVE")
	copy(result[12:16], "fmt ")
	binary.LittleEndian.PutUint32(result[16:20], 16)
	binary.LittleEndian.PutUint16(result[20:22], 1)
	binary.LittleEndian.PutUint16(result[22:24], 1)
	binary.LittleEndian.PutUint32(result[24:28], 24000)
	binary.LittleEndian.PutUint32(result[28:32], 48000)
	binary.LittleEndian.PutUint16(result[32:34], 2)
	binary.LittleEndian.PutUint16(result[34:36], 16)
	copy(result[36:40], "data")
	binary.LittleEndian.PutUint32(result[40:44], uint32(len(pcm)))
	copy(result[44:], pcm)
	return result
}

func chatSSEData(line []byte) (string, bool) {
	text := strings.TrimSpace(string(line))
	if !strings.HasPrefix(text, "data:") {
		return "", false
	}
	data := strings.TrimSpace(strings.TrimPrefix(text, "data:"))
	return data, data != ""
}

func decodeMiMoTTSAudioChunk(value string) ([]byte, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err == nil {
		return decoded, nil
	}
	decoded, rawErr := base64.RawStdEncoding.DecodeString(value)
	if rawErr != nil {
		return nil, fmt.Errorf("decode MiMo TTS audio chunk: %w", err)
	}
	return decoded, nil
}
