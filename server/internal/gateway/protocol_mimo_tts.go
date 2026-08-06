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

type mimoTTSChatStreamChunk struct {
	Audio []byte
	Usage *completionUsage
	Done  bool
}

type mimoTTSChatStreamDecoder struct {
	id             string
	model          string
	created        int64
	role           string
	content        strings.Builder
	finishReason   *string
	usage          completionUsage
	hasUsage       bool
	receivedChoice bool
	receivedDone   bool
	audioBytes     int
	collectAudio   bool
	audio          bytes.Buffer
}

func isMiMoV25TTSModel(model string) bool {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "mimo-v2.5-tts", "mimo-v2.5-tts-voicedesign", "mimo-v2.5-tts-voiceclone":
		return true
	default:
		return false
	}
}

func newMiMoTTSChatStreamDecoder(collectAudio bool) *mimoTTSChatStreamDecoder {
	return &mimoTTSChatStreamDecoder{collectAudio: collectAudio}
}

func (d *mimoTTSChatStreamDecoder) ConsumeLine(line []byte) (mimoTTSChatStreamChunk, error) {
	data, ok := chatSSEData(line)
	if !ok {
		return mimoTTSChatStreamChunk{}, nil
	}
	if data == "[DONE]" {
		d.receivedDone = true
		return mimoTTSChatStreamChunk{Done: true}, nil
	}
	var event mimoTTSChatStreamEvent
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return mimoTTSChatStreamChunk{}, err
	}
	if len(event.Error) > 0 && string(event.Error) != "null" {
		return mimoTTSChatStreamChunk{}, fmt.Errorf("upstream stream error: %s", string(event.Error))
	}
	d.id = nonEmptyString(event.ID, d.id)
	d.model = nonEmptyString(event.Model, d.model)
	if event.Created != 0 {
		d.created = event.Created
	}
	var audio bytes.Buffer
	for _, choice := range event.Choices {
		if choice.Index != 0 {
			continue
		}
		d.receivedChoice = true
		d.role = nonEmptyString(choice.Delta.Role, d.role)
		d.content.WriteString(choice.Delta.Content)
		if choice.FinishReason != nil {
			d.finishReason = choice.FinishReason
		}
		decoded, err := decodeMiMoTTSAudioChunk(choice.Delta.Audio.Data)
		if err != nil {
			return mimoTTSChatStreamChunk{}, err
		}
		audio.Write(decoded)
	}
	chunk := audio.Bytes()
	if len(chunk) > 0 {
		d.audioBytes += len(chunk)
		if d.collectAudio {
			d.audio.Write(chunk)
		}
	}
	var usage *completionUsage
	if event.Usage != nil {
		normalized := event.Usage.normalized()
		d.usage = normalized
		d.hasUsage = true
		usage = &normalized
	}
	return mimoTTSChatStreamChunk{Audio: chunk, Usage: usage}, nil
}

func (d *mimoTTSChatStreamDecoder) Finalize() ([]byte, completionUsage, error) {
	if !d.receivedChoice {
		return nil, completionUsage{}, fmt.Errorf("upstream stream ended before any completion choice was received")
	}
	if !d.receivedDone && d.finishReason == nil {
		return nil, completionUsage{}, fmt.Errorf("upstream stream ended before completion")
	}
	if d.audioBytes == 0 {
		return nil, completionUsage{}, fmt.Errorf("upstream stream ended without audio data")
	}
	if !d.collectAudio {
		return nil, d.usage, nil
	}
	return d.audio.Bytes(), d.usage, nil
}

func (d *mimoTTSChatStreamDecoder) responseBody(responseFormat string) ([]byte, completionUsage, error) {
	audioData, usage, err := d.Finalize()
	if err != nil {
		return nil, completionUsage{}, err
	}
	message := map[string]any{"role": nonEmptyString(d.role, "assistant"), "content": nil}
	if d.content.Len() > 0 {
		message["content"] = d.content.String()
	}
	if strings.EqualFold(strings.TrimSpace(responseFormat), "wav") {
		audioData = mimoTTSWAV(audioData)
	}
	message["audio"] = map[string]any{"data": base64.StdEncoding.EncodeToString(audioData)}
	response := map[string]any{
		"id":      d.id,
		"object":  "chat.completion",
		"created": d.created,
		"model":   d.model,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       message,
			"finish_reason": d.finishReason,
		}},
	}
	if d.hasUsage {
		response["usage"] = usage
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return nil, completionUsage{}, err
	}
	return encoded, usage, nil
}

func bufferMiMoTTSChatStream(body []byte, responseFormat string) ([]byte, completionUsage, error) {
	decoder := newMiMoTTSChatStreamDecoder(true)
	if err := consumeMiMoTTSChatStreamBody(body, decoder); err != nil {
		return nil, completionUsage{}, err
	}
	return decoder.responseBody(responseFormat)
}

func consumeMiMoTTSChatStreamBody(body []byte, decoder *mimoTTSChatStreamDecoder) error {
	reader := bufio.NewReader(bytes.NewReader(body))
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if _, consumeErr := decoder.ConsumeLine(line); consumeErr != nil {
				return consumeErr
			}
		}
		if err == nil {
			continue
		}
		if err != io.EOF {
			return err
		}
		return nil
	}
}

func bufferMiMoTTSChatAudio(body []byte, responseFormat string) ([]byte, completionUsage, error) {
	decoder := newMiMoTTSChatStreamDecoder(true)
	if err := consumeMiMoTTSChatStreamBody(body, decoder); err != nil {
		return nil, completionUsage{}, err
	}
	audio, usage, err := decoder.Finalize()
	if err != nil {
		return nil, completionUsage{}, err
	}
	if strings.EqualFold(strings.TrimSpace(responseFormat), "wav") {
		audio = mimoTTSWAV(audio)
	}
	return audio, usage, nil
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
