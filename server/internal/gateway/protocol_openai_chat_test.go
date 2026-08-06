package gateway

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	routeengine "xlyra/server/internal/router"
)

func TestOpenAIChatProtocolBuildsPayloadPathAndStreamOptions(t *testing.T) {
	t.Parallel()

	candidate := routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "openai"},
		Model: routeengine.CandidateModel{UpstreamName: "gpt-5.1"},
	}
	protocol := newOpenAIChatProtocolAdapter(gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions}, candidate)
	payload, err := protocol.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointChatCompletions,
		Payload: map[string]any{
			"model":  "alias-chat",
			"stream": true,
			"messages": []any{
				map[string]any{"role": "user", "content": "hello"},
			},
		},
	}, candidate)
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	if payload["model"] != "gpt-5.1" {
		t.Fatalf("model = %#v, want upstream model", payload["model"])
	}
	streamOptions, ok := payload["stream_options"].(map[string]any)
	if !ok || streamOptions["include_usage"] != true {
		t.Fatalf("stream_options = %#v, want include_usage true", payload["stream_options"])
	}
	if got := protocol.UpstreamPath("https://api.example.test"); got != "https://api.example.test/v1/chat/completions" {
		t.Fatalf("UpstreamPath = %q, want chat completions path", got)
	}
	if got := protocol.ProtocolName(); got != "openai_chat_completions" {
		t.Fatalf("ProtocolName = %q, want openai_chat_completions", got)
	}
}

func TestMiMoV25TTSChatProtocolForcesUpstreamStream(t *testing.T) {
	t.Parallel()

	for _, model := range []string{"mimo-v2.5-tts", "mimo-v2.5-tts-voicedesign", "mimo-v2.5-tts-voiceclone"} {
		model := model
		t.Run(model, func(t *testing.T) {
			t.Parallel()
			candidate := routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: model}}
			protocol := newOpenAIChatProtocolAdapter(gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions}, candidate)
			payload, err := protocol.BuildUpstreamPayload(gatewayRequest{
				DownstreamPath: gatewayEndpointChatCompletions,
				Payload: map[string]any{
					"model":                 "alias",
					"stream":                false,
					"max_completion_tokens": 42,
					"messages":              []any{map[string]any{"role": "assistant", "content": "hello"}},
					"audio":                 map[string]any{"format": "wav", "voice": "mimo_default"},
				},
			}, candidate)
			if err != nil {
				t.Fatalf("BuildUpstreamPayload returned error: %v", err)
			}
			if payload["stream"] != true || !protocol.usesRawBufferedResponse() {
				t.Fatalf("payload stream = %#v, raw buffering = %v", payload["stream"], protocol.usesRawBufferedResponse())
			}
			if _, ok := payload["audio"].(map[string]any); !ok {
				t.Fatalf("audio payload was not preserved: %#v", payload)
			}
			if payload["audio"].(map[string]any)["format"] != "pcm16" {
				t.Fatalf("audio format = %#v, want forced pcm16", payload["audio"].(map[string]any)["format"])
			}
			if _, ok := payload["max_tokens"]; ok {
				t.Fatalf("MiMo TTS payload retained max_tokens: %#v", payload)
			}
			if _, ok := payload["max_completion_tokens"]; ok {
				t.Fatalf("MiMo TTS payload retained max_completion_tokens: %#v", payload)
			}
		})
	}

	regular := newOpenAIChatProtocolAdapter(gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions}, routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "gpt-5.1"}})
	if regular.usesRawBufferedResponse() {
		t.Fatal("regular chat model must not use MiMo TTS buffering")
	}
}

func TestMiMoV25TTSChatProtocolRejectsNonPCM16StreamingFormat(t *testing.T) {
	t.Parallel()

	candidate := routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "mimo-v2.5-tts"}}
	for _, format := range []string{"", "wav", "mp3"} {
		request := gatewayRequest{
			DownstreamPath: gatewayEndpointChatCompletions,
			Stream:         true,
			Payload: map[string]any{
				"model":    "mimo-v2.5-tts",
				"stream":   true,
				"messages": []any{map[string]any{"role": "assistant", "content": "hello"}},
				"audio":    map[string]any{"format": format, "voice": "mimo_default"},
			},
		}
		protocol := newOpenAIChatProtocolAdapter(request, candidate)
		if _, err := protocol.BuildUpstreamPayload(request, candidate); err == nil {
			t.Fatalf("streaming format %q must be rejected", format)
		}
	}

	request := gatewayRequest{
		DownstreamPath: gatewayEndpointChatCompletions,
		Stream:         true,
		Payload: map[string]any{
			"model":    "mimo-v2.5-tts",
			"stream":   true,
			"messages": []any{map[string]any{"role": "assistant", "content": "hello"}},
			"audio":    map[string]any{"format": " PCM16 ", "voice": "mimo_default"},
		},
	}
	protocol := newOpenAIChatProtocolAdapter(request, candidate)
	payload, err := protocol.BuildUpstreamPayload(request, candidate)
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	if payload["audio"].(map[string]any)["format"] != "pcm16" {
		t.Fatalf("audio format = %#v, want pcm16", payload["audio"].(map[string]any)["format"])
	}
}

func TestMiMoV25TTSChatProtocolBuffersStreamForNonStreamingDownstream(t *testing.T) {
	t.Parallel()

	first := base64.StdEncoding.EncodeToString([]byte("first-"))
	second := base64.StdEncoding.EncodeToString([]byte("second"))
	body := []byte(
		`data: {"id":"chatcmpl_mimo","object":"chat.completion.chunk","created":123,"model":"mimo-v2.5-tts","choices":[{"index":0,"delta":{"role":"assistant","audio":{"data":"` + first + `"}},"finish_reason":null}]}` + "\n\n" +
			`data: {"id":"chatcmpl_mimo","object":"chat.completion.chunk","created":123,"model":"mimo-v2.5-tts","choices":[{"index":0,"delta":{"audio":{"data":"` + second + `"}},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":6,"total_tokens":10}}` + "\n\n" +
			"data: [DONE]\n\n",
	)
	protocol := newOpenAIChatProtocolAdapter(
		gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions, Payload: map[string]any{"audio": map[string]any{"format": "wav"}}},
		routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "mimo-v2.5-tts"}},
	)
	transformed, err := protocol.TransformBufferedResponse(http.StatusOK, http.Header{"Content-Type": []string{"text/event-stream"}}, body)
	if err != nil {
		t.Fatalf("TransformBufferedResponse returned error: %v", err)
	}
	if transformed.ContentType != "application/json" || transformed.Usage.TotalTokens != 10 {
		t.Fatalf("unexpected transformed response: %#v", transformed)
	}
	var response struct {
		Object  string `json:"object"`
		Choices []struct {
			Message struct {
				Role  string `json:"role"`
				Audio struct {
					Data string `json:"data"`
				} `json:"audio"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(transformed.Body, &response); err != nil {
		t.Fatalf("decode transformed body: %v", err)
	}
	if response.Object != "chat.completion" || len(response.Choices) != 1 || response.Choices[0].Message.Role != "assistant" || response.Choices[0].FinishReason != "stop" {
		t.Fatalf("unexpected completion envelope: %#v", response)
	}
	audio, err := base64.StdEncoding.DecodeString(response.Choices[0].Message.Audio.Data)
	if err != nil {
		t.Fatalf("decode buffered audio: %v", err)
	}
	if len(audio) != 44+len("first-second") || string(audio[:4]) != "RIFF" || string(audio[8:12]) != "WAVE" || string(audio[44:]) != "first-second" {
		t.Fatalf("unexpected WAV audio: %q", string(audio))
	}
}

func TestMiMoV25TTSChatProtocolRejectsIncompleteBufferedStream(t *testing.T) {
	t.Parallel()

	audio := base64.StdEncoding.EncodeToString([]byte("partial"))
	for _, body := range []string{
		`data: {"choices":[{"index":0,"delta":{"audio":{"data":"` + audio + `"}},"finish_reason":null}]}` + "\n\n",
		`data: {"choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":"stop"}]}` + "\n\n" + "data: [DONE]\n\n",
	} {
		protocol := newOpenAIChatProtocolAdapter(
			gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions, Payload: map[string]any{"audio": map[string]any{"format": "wav"}}},
			routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "mimo-v2.5-tts"}},
		)
		if _, err := protocol.TransformBufferedResponse(http.StatusOK, http.Header{"Content-Type": []string{"text/event-stream"}}, []byte(body)); err == nil {
			t.Fatalf("incomplete buffered stream must be rejected: %q", body)
		}
	}
}

func TestMiMoV25TTSChatProtocolPassesStreamThrough(t *testing.T) {
	t.Parallel()

	chunk := `data: {"id":"chatcmpl_mimo","choices":[{"index":0,"delta":{"audio":{"data":"Zm9v"}},"finish_reason":null}]}` + "\n\n"
	body := chunk + "data: [DONE]\n\n"
	protocol := newOpenAIChatProtocolAdapter(
		gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions, Stream: true, Payload: map[string]any{"audio": map[string]any{"format": "pcm16"}}},
		routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "mimo-v2.5-tts"}},
	)
	recorder := httptest.NewRecorder()
	capture, started, err := protocol.ProxyStream(t.Context(), recorder, gatewayStreamTestResponse(body), time.Now(), routeengine.Candidate{})
	if err != nil {
		t.Fatalf("ProxyStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted || recorder.Body.String() != body {
		t.Fatalf("started=%v capture=%+v body=%q", started, capture, recorder.Body.String())
	}
}

func TestOpenAIChatProtocolConvertsResponsesRequestAndResponse(t *testing.T) {
	t.Parallel()

	candidate := routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "gpt-5.1"}}
	protocol := newOpenAIChatProtocolAdapter(gatewayRequest{DownstreamPath: gatewayEndpointResponses}, candidate)

	payload, err := protocol.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointResponses,
		Payload: map[string]any{
			"model":             "alias-responses",
			"input":             "hello from responses",
			"max_output_tokens": 64.0,
		},
	}, candidate)
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	if payload["model"] != "gpt-5.1" || payload["max_tokens"] != 64 {
		t.Fatalf("unexpected converted payload: %#v", payload)
	}
	if _, ok := payload["messages"].([]any); !ok {
		t.Fatalf("converted payload missing messages: %#v", payload)
	}
	if got := protocol.ProtocolName(); got != "openai_chat_completions_to_responses" {
		t.Fatalf("ProtocolName = %q, want responses conversion", got)
	}

	body := []byte(`{"id":"chatcmpl_1","object":"chat.completion","model":"gpt-5.1","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`)
	transformed, err := protocol.TransformBufferedResponse(http.StatusOK, http.Header{"Content-Type": []string{"application/json"}}, body)
	if err != nil {
		t.Fatalf("TransformBufferedResponse returned error: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(transformed.Body, &out); err != nil {
		t.Fatalf("decode transformed response: %v", err)
	}
	if out["object"] != "response" {
		t.Fatalf("object = %#v, want response; body=%s", out["object"], string(transformed.Body))
	}
	if transformed.ContentType != "application/json" || transformed.Usage.TotalTokens != 5 {
		t.Fatalf("unexpected transformed metadata: %#v", transformed)
	}
}

func TestOpenAIChatProtocolTransformsNativeAndStreams(t *testing.T) {
	t.Parallel()

	body := []byte(`{"choices":[{"message":{"role":"assistant","content":"hi"}}],"usage":{"prompt_tokens":4,"completion_tokens":6,"total_tokens":10}}`)
	transformed, err := (openAIChatProtocolAdapter{}).TransformBufferedResponse(http.StatusOK, http.Header{"Content-Type": []string{"application/json"}}, body)
	if err != nil {
		t.Fatalf("TransformBufferedResponse returned error: %v", err)
	}
	if transformed.ContentType != "application/json" || transformed.Usage.PromptTokens != 4 || transformed.Usage.TotalTokens != 10 || string(transformed.Body) != string(body) {
		t.Fatalf("unexpected native transform: %#v", transformed)
	}

	resp := gatewayStreamTestResponse(
		"data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\n" +
			"data: {\"choices\":[{\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2,\"total_tokens\":3}}\n\n" +
			"data: [DONE]\n\n")
	rec := httptest.NewRecorder()
	capture, started, err := (openAIChatProtocolAdapter{}).ProxyStream(t.Context(), rec, resp, time.Now(), routeengine.Candidate{})
	if err != nil {
		t.Fatalf("ProxyStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted || capture.usage.TotalTokens != 3 {
		t.Fatalf("started=%v capture=%+v", started, capture)
	}
	assertGatewayBodyContainsAll(t, rec.Body.String(), `"delta":{"content":"Hi"}`)
}
