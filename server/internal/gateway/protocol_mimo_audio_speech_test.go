package gateway

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	routeengine "xlyra/server/internal/router"
)

func mimoAudioSpeechCandidate(model string) routeengine.Candidate {
	return routeengine.Candidate{Site: routeengine.CandidateSite{BaseURL: "https://api.example.test"}, Model: routeengine.CandidateModel{UpstreamName: model}}
}

func mimoChatSSEBody(audioChunks ...string) string {
	var body strings.Builder
	body.WriteString(`data: {"id":"mimo-msg","created":123,"model":"mimo-v2.5-tts","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}` + "\n\n")
	for _, chunk := range audioChunks {
		body.WriteString(`data: {"choices":[{"index":0,"delta":{"audio":{"data":"`)
		body.WriteString(base64.StdEncoding.EncodeToString([]byte(chunk)))
		body.WriteString(`"}},"finish_reason":null}]}` + "\n\n")
	}
	body.WriteString(`data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":6,"total_tokens":10}}` + "\n\n")
	body.WriteString("data: [DONE]\n\n")
	return body.String()
}

func TestMiMoAudioSpeechBuildsChatPayload(t *testing.T) {
	t.Parallel()

	request := gatewayRequest{
		DownstreamPath: gatewayEndpointAudioSpeech,
		Payload: map[string]any{
			"model":           "mimo-v2.5-tts",
			"input":           "hello",
			"voice":           "Chloe",
			"instructions":    "warm and clear",
			"response_format": "wav",
		},
		Stream: true,
	}
	candidate := mimoAudioSpeechCandidate("mimo-v2.5-tts")
	adapter := newMiMoAudioSpeechProtocolAdapter(request, candidate)
	payload, err := adapter.BuildUpstreamPayload(request, candidate)
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	if payload["model"] != "mimo-v2.5-tts" || payload["stream"] != true {
		t.Fatalf("payload envelope = %#v", payload)
	}
	messages := payload["messages"].([]any)
	if len(messages) != 2 || messages[0].(map[string]any)["role"] != "user" || messages[1].(map[string]any)["role"] != "assistant" {
		t.Fatalf("messages = %#v", messages)
	}
	audio := payload["audio"].(map[string]any)
	if audio["format"] != "pcm16" || audio["voice"] != "Chloe" {
		t.Fatalf("audio = %#v", audio)
	}
	if got := adapter.UpstreamPath(candidate.Site.BaseURL); got != "https://api.example.test/v1/chat/completions" {
		t.Fatalf("upstream path = %q", got)
	}
}

func TestMiMoAudioSpeechValidatesModelSpecificFields(t *testing.T) {
	t.Parallel()

	voiceDesign := gatewayRequest{
		Payload: map[string]any{
			"input":           "hello",
			"response_format": "pcm",
		},
		Stream: true,
	}
	voiceDesignCandidate := mimoAudioSpeechCandidate("mimo-v2.5-tts-voicedesign")
	if _, err := newMiMoAudioSpeechProtocolAdapter(voiceDesign, voiceDesignCandidate).BuildUpstreamPayload(voiceDesign, voiceDesignCandidate); err == nil {
		t.Fatal("voice design without instructions must be rejected")
	}

	unsupported := voiceDesign
	unsupported.Payload = map[string]any{"input": "hello", "response_format": "mp3"}
	baseCandidate := mimoAudioSpeechCandidate("mimo-v2.5-tts")
	if _, err := newMiMoAudioSpeechProtocolAdapter(unsupported, baseCandidate).BuildUpstreamPayload(unsupported, baseCandidate); err == nil {
		t.Fatal("mp3 response format must be rejected")
	}

	voiceClone := gatewayRequest{Payload: map[string]any{"input": "hello", "response_format": "pcm"}, Stream: true}
	voiceCloneCandidate := mimoAudioSpeechCandidate("mimo-v2.5-tts-voiceclone")
	if _, err := newMiMoAudioSpeechProtocolAdapter(voiceClone, voiceCloneCandidate).BuildUpstreamPayload(voiceClone, voiceCloneCandidate); err == nil {
		t.Fatal("voice clone without voice must be rejected")
	}

	invalidInstructions := gatewayRequest{Payload: map[string]any{"input": "hello", "instructions": true, "response_format": "pcm"}, Stream: true}
	if _, err := newMiMoAudioSpeechProtocolAdapter(invalidInstructions, baseCandidate).BuildUpstreamPayload(invalidInstructions, baseCandidate); err == nil {
		t.Fatal("non-string instructions must be rejected")
	}
}

func TestMiMoAudioSpeechProxyStreamWritesPCM(t *testing.T) {
	t.Parallel()

	request := gatewayRequest{Payload: map[string]any{"response_format": "pcm"}, Stream: true}
	adapter := newMiMoAudioSpeechProtocolAdapter(request, mimoAudioSpeechCandidate("mimo-v2.5-tts"))
	recorder := httptest.NewRecorder()
	capture, started, err := adapter.ProxyStream(t.Context(), recorder, &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(mimoChatSSEBody("first-", "second"))),
	}, time.Now(), routeengine.Candidate{})
	if err != nil || !started || !capture.streamCompleted {
		t.Fatalf("capture=%+v started=%v err=%v", capture, started, err)
	}
	if recorder.Header().Get("Content-Type") != "audio/pcm" || recorder.Body.String() != "first-second" {
		t.Fatalf("content-type=%q body=%q", recorder.Header().Get("Content-Type"), recorder.Body.String())
	}
}

func TestMiMoAudioSpeechProxyStreamRejectsEmptyAudio(t *testing.T) {
	t.Parallel()

	request := gatewayRequest{Payload: map[string]any{"response_format": "pcm"}, Stream: true}
	adapter := newMiMoAudioSpeechProtocolAdapter(request, mimoAudioSpeechCandidate("mimo-v2.5-tts"))
	recorder := httptest.NewRecorder()
	_, started, err := adapter.ProxyStream(t.Context(), recorder, &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")),
	}, time.Now(), routeengine.Candidate{})
	if err == nil || started {
		t.Fatalf("empty audio should fail before a successful response: started=%v err=%v", started, err)
	}
}

func TestMiMoAudioSpeechProxyStreamWritesWAVAndSSE(t *testing.T) {
	t.Parallel()

	body := mimoChatSSEBody("audio")
	for _, downstreamSSE := range []bool{false, true} {
		request := gatewayRequest{Payload: map[string]any{"response_format": "wav", "stream_format": map[bool]string{true: "sse", false: "audio"}[downstreamSSE]}, Stream: true}
		adapter := newMiMoAudioSpeechProtocolAdapter(request, mimoAudioSpeechCandidate("mimo-v2.5-tts"))
		recorder := httptest.NewRecorder()
		capture, started, err := adapter.ProxyStream(t.Context(), recorder, &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, time.Now(), routeengine.Candidate{})
		if err != nil || !started || !capture.streamCompleted {
			t.Fatalf("sse=%v capture=%+v started=%v err=%v", downstreamSSE, capture, started, err)
		}
		if downstreamSSE {
			if !strings.Contains(recorder.Body.String(), `"type":"speech.audio.delta"`) || !strings.Contains(recorder.Body.String(), `"type":"speech.audio.done"`) {
				t.Fatalf("SSE body = %q", recorder.Body.String())
			}
		} else if !strings.HasPrefix(recorder.Body.String(), "RIFF") {
			t.Fatalf("WAV body = %q", recorder.Body.String())
		}
	}
}

func TestMiMoAudioSpeechResolverUsesBridge(t *testing.T) {
	t.Parallel()

	request := gatewayRequest{DownstreamPath: gatewayEndpointAudioSpeech, Payload: map[string]any{"input": "hello", "response_format": "pcm"}, Stream: true}
	adapter, err := (openAIProtocolResolver{}).Resolve(t.Context(), request, mimoAudioSpeechCandidate("mimo-v2.5-tts"))
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if _, ok := adapter.(mimoAudioSpeechProtocolAdapter); !ok {
		t.Fatalf("adapter type = %T, want mimoAudioSpeechProtocolAdapter", adapter)
	}
}
