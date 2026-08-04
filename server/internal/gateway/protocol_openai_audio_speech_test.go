package gateway

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	routeengine "xlyra/server/internal/router"
)

func audioSSEBody(chunks []string, inputTokens, outputTokens int) string {
	var b strings.Builder
	for _, chunk := range chunks {
		encoded := base64.StdEncoding.EncodeToString([]byte(chunk))
		b.WriteString("data: {\"type\":\"speech.audio.delta\",\"audio\":\"" + encoded + "\"}\n\n")
	}
	b.WriteString("data: {\"type\":\"speech.audio.done\",\"usage\":{\"input_tokens\":")
	b.WriteString(strconv.Itoa(inputTokens))
	b.WriteString(",\"output_tokens\":")
	b.WriteString(strconv.Itoa(outputTokens))
	b.WriteString(",\"total_tokens\":")
	b.WriteString(strconv.Itoa(inputTokens + outputTokens))
	b.WriteString("}}\n\n")
	return b.String()
}

func TestAudioSpeechModelSupportsSSE(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"gpt-4o-mini-tts": true,
		"gpt-4o-audio":    true,
		"tts-1":           false,
		"tts-1-hd":        false,
		"TTS-1":           false,
	}
	for model, want := range cases {
		if got := audioSpeechModelSupportsSSE(model); got != want {
			t.Fatalf("audioSpeechModelSupportsSSE(%q) = %v, want %v", model, got, want)
		}
	}
}

func TestAudioSpeechSpecAllowsSSERespectsRequestPolicy(t *testing.T) {
	t.Parallel()

	if audioSpeechSpecAllowsSSE(resolvedProtocolSpec{RequestParams: requestParamPolicy{Unsupported: []string{"stream_format"}}}) {
		t.Fatal("unsupported stream_format must disable internal SSE")
	}
	if audioSpeechSpecAllowsSSE(resolvedProtocolSpec{RequestParams: requestParamPolicy{Fixed: map[string]any{"stream_format": "audio"}}}) {
		t.Fatal("fixed audio stream_format must disable internal SSE")
	}
	if !audioSpeechSpecAllowsSSE(resolvedProtocolSpec{}) {
		t.Fatal("stream_format must remain available without an overriding policy")
	}
}

func TestAudioSpeechBuildUpstreamPayloadForcesSSE(t *testing.T) {
	t.Parallel()

	adapter := openAIAudioSpeechProtocolAdapter{sseMode: true}
	request := gatewayRequest{Payload: map[string]any{"model": "downstream", "input": "hello", "voice": "alloy"}}
	candidate := routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "gpt-4o-mini-tts"}}

	payload, err := adapter.BuildUpstreamPayload(request, candidate)
	if err != nil {
		t.Fatalf("BuildUpstreamPayload error: %v", err)
	}
	if payload["stream_format"] != "sse" {
		t.Fatalf("stream_format = %v, want sse", payload["stream_format"])
	}
	if payload["model"] != "gpt-4o-mini-tts" {
		t.Fatalf("model = %v, want upstream name", payload["model"])
	}
	if payload["voice"] != "alloy" {
		t.Fatalf("voice = %v, want passthrough", payload["voice"])
	}

	ttsCandidate := routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "tts-1"}}
	ttsRequest := gatewayRequest{Payload: map[string]any{"model": "mapped-alias", "input": "hello", "stream_format": "sse"}}
	ttsAdapter := newOpenAIAudioSpeechProtocolAdapter(ttsRequest, ttsCandidate)
	if _, err := ttsAdapter.BuildUpstreamPayload(ttsRequest, ttsCandidate); err == nil {
		t.Fatal("tts-1 must reject downstream stream_format=sse")
	}
}

func TestAudioSpeechBuildUpstreamPayloadAppliesModelPolicy(t *testing.T) {
	t.Parallel()

	request := gatewayRequest{Payload: map[string]any{
		"model":           "alias",
		"input":           "hello",
		"voice":           "mimo_default",
		"response_format": "wav",
		"tools":           []any{map[string]any{"type": "function"}},
	}}
	candidate := routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "xlyra"},
		Model: routeengine.CandidateModel{UpstreamName: "mimo-v2-tts-preview"},
	}
	adapter := newOpenAIAudioSpeechProtocolAdapter(request, candidate)
	if adapter.responseFormat != audioSpeechDefaultResponseFormat {
		t.Fatalf("responseFormat = %q, want policy-adjusted default %q", adapter.responseFormat, audioSpeechDefaultResponseFormat)
	}
	payload, err := adapter.BuildUpstreamPayload(request, candidate)
	if err != nil {
		t.Fatalf("BuildUpstreamPayload error: %v", err)
	}
	for _, key := range []string{"response_format", "tools"} {
		if _, ok := payload[key]; ok {
			t.Fatalf("%s must be removed by model policy, got %#v", key, payload[key])
		}
	}
	if payload["stream_format"] != "sse" {
		t.Fatalf("stream_format = %#v, want internal sse", payload["stream_format"])
	}
}

func TestAudioSpeechApplyUpstreamHeadersSelectsResponseMode(t *testing.T) {
	t.Parallel()

	sseRequest := httptest.NewRequest(http.MethodPost, "https://upstream.test/v1/audio/speech", nil)
	(openAIAudioSpeechProtocolAdapter{sseMode: true}).ApplyUpstreamHeaders(sseRequest, "", "", true)
	if accept := sseRequest.Header.Get("Accept"); accept != "text/event-stream" {
		t.Fatalf("SSE Accept = %q", accept)
	}
	rawRequest := httptest.NewRequest(http.MethodPost, "https://upstream.test/v1/audio/speech", nil)
	(openAIAudioSpeechProtocolAdapter{}).ApplyUpstreamHeaders(rawRequest, "", "", true)
	if accept := rawRequest.Header.Get("Accept"); accept != "application/octet-stream" {
		t.Fatalf("raw Accept = %q", accept)
	}
}

func TestCollectAudioSpeechFromSSEAccumulatesAndCapturesUsage(t *testing.T) {
	t.Parallel()

	body := audioSSEBody([]string{"AAA", "BBB", "CCC"}, 14, 101)
	audio, usage := collectAudioSpeechFromSSE([]byte(body))

	if string(audio) != "AAABBBCCC" {
		t.Fatalf("accumulated audio = %q, want AAABBBCCC", string(audio))
	}
	if usage.PromptTokens != 14 || usage.AudioOutputTokens != 101 || usage.TotalTokens != 115 {
		t.Fatalf("usage = %+v, want input=14 audio_out=101 total=115", usage)
	}
}

func TestTransformBufferedResponseSSEToBinary(t *testing.T) {
	t.Parallel()

	adapter := openAIAudioSpeechProtocolAdapter{sseMode: true, responseFormat: "mp3"}
	headers := http.Header{}
	headers.Set("Content-Type", "text/event-stream")
	body := audioSSEBody([]string{"audio-bytes"}, 10, 50)

	transformed, err := adapter.TransformBufferedResponse(http.StatusOK, headers, []byte(body))
	if err != nil {
		t.Fatalf("TransformBufferedResponse error: %v", err)
	}
	if transformed.ContentType != "audio/mpeg" {
		t.Fatalf("content type = %q, want audio/mpeg", transformed.ContentType)
	}
	if string(transformed.Body) != "audio-bytes" {
		t.Fatalf("body = %q, want decoded binary", string(transformed.Body))
	}
	if transformed.Usage.PromptTokens != 10 || transformed.Usage.AudioOutputTokens != 50 {
		t.Fatalf("usage = %+v, want input=10 audio_out=50", transformed.Usage)
	}
}

func TestTransformBufferedResponseTTS1CharBilling(t *testing.T) {
	t.Parallel()

	adapter := openAIAudioSpeechProtocolAdapter{sseMode: false, responseFormat: "mp3", inputChars: 42}
	headers := http.Header{}
	headers.Set("Content-Type", "audio/mpeg")

	transformed, err := adapter.TransformBufferedResponse(http.StatusOK, headers, []byte("raw-binary-audio"))
	if err != nil {
		t.Fatalf("TransformBufferedResponse error: %v", err)
	}
	if string(transformed.Body) != "raw-binary-audio" {
		t.Fatalf("body = %q, want binary passthrough", string(transformed.Body))
	}
	if transformed.Usage.PromptTokens != 42 || transformed.Usage.AudioOutputTokens != 0 {
		t.Fatalf("usage = %+v, want char count 42 in prompt tokens, no audio out", transformed.Usage)
	}
}

func TestTransformBufferedResponsePassesThroughErrors(t *testing.T) {
	t.Parallel()

	adapter := openAIAudioSpeechProtocolAdapter{sseMode: true}
	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	transformed, _ := adapter.TransformBufferedResponse(http.StatusBadRequest, headers, []byte(`{"error":{"message":"bad voice"}}`))
	if transformed.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 passthrough", transformed.StatusCode)
	}
	if string(transformed.Body) != `{"error":{"message":"bad voice"}}` {
		t.Fatalf("error body must pass through unchanged, got %q", string(transformed.Body))
	}
}

func TestAudioSpeechContentType(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"mp3":  "audio/mpeg",
		"":     "audio/mpeg",
		"opus": "audio/ogg",
		"aac":  "audio/aac",
		"flac": "audio/flac",
		"wav":  "audio/wav",
		"pcm":  "audio/pcm",
	}
	for format, want := range cases {
		if got := audioSpeechContentType(format); got != want {
			t.Fatalf("audioSpeechContentType(%q) = %q, want %q", format, got, want)
		}
	}
}

func TestAudioOutputUnitValueChainsRatios(t *testing.T) {
	t.Parallel()

	input := 0.60
	audioRatio := 20.0
	audioCompletion := 1.0
	pricing := selectedPricing{InputValue: &input, AudioRatio: &audioRatio, AudioCompletionRatio: &audioCompletion}
	value, ok := audioOutputUnitValue(pricing)
	if !ok || value != 12.0 {
		t.Fatalf("audio output value = (%v, %v), want $12 per 1M", value, ok)
	}

	usage := gatewayUsage{PromptTokens: 14, AudioOutputTokens: 101}
	cost := estimateCost(usage, pricing)
	if cost == nil {
		t.Fatal("expected cost")
	}
	expected := 14*0.60/1_000_000 + 101*12.0/1_000_000
	if diff := *cost - expected; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("cost = %v, want %v (text input + audio output)", *cost, expected)
	}

	if _, ok := audioOutputUnitValue(selectedPricing{InputValue: &input}); ok {
		t.Fatal("audio output value must require AudioRatio")
	}
}

func TestAudioSpeechCharBillingCost(t *testing.T) {
	t.Parallel()

	input := 15.0
	pricing := selectedPricing{InputValue: &input}
	cost := estimateCost(gatewayUsage{PromptTokens: 4096}, pricing)
	if cost == nil {
		t.Fatal("expected cost")
	}
	expected := 4096 * 15.0 / 1_000_000
	if diff := *cost - expected; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("char billing cost = %v, want %v", *cost, expected)
	}
}

func TestAudioSpeechEndpointResolvesToAudioAdapter(t *testing.T) {
	t.Parallel()

	adapter, err := (openAIProtocolResolver{}).Resolve(context.Background(), gatewayRequest{
		DownstreamPath: gatewayEndpointAudioSpeech,
		Payload:        map[string]any{"input": "hello world", "response_format": "opus"},
	}, routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "gpt-4o-mini-tts"}})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got := adapter.ProtocolName(); got != "openai_audio_speech" {
		t.Fatalf("ProtocolName = %q, want openai_audio_speech", got)
	}
	audio, ok := adapter.(openAIAudioSpeechProtocolAdapter)
	if !ok {
		t.Fatalf("adapter type = %T, want openAIAudioSpeechProtocolAdapter", adapter)
	}
	if !audio.sseMode {
		t.Fatal("gpt-4o-mini-tts must resolve to SSE mode")
	}
	if audio.inputChars != 11 {
		t.Fatalf("inputChars = %d, want 11", audio.inputChars)
	}
	if audio.responseFormat != "opus" {
		t.Fatalf("responseFormat = %q, want opus", audio.responseFormat)
	}
}

func TestInferCanonicalProtocolRecognizesAudioSpeech(t *testing.T) {
	t.Parallel()

	if protocol := inferCanonicalProtocol("openai_audio_speech"); protocol != canonicalProtocolOpenAIAudioSpeech {
		t.Fatalf("protocol = %q, want %q", protocol, canonicalProtocolOpenAIAudioSpeech)
	}
}

func TestAudioSpeechEndpointAdapterDecodeRequest(t *testing.T) {
	t.Parallel()

	body := `{"model":"gpt-4o-mini-tts","input":"speak this","voice":"alloy","stream_format":"sse"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/audio/speech", strings.NewReader(body))
	decoded, failure := audioSpeechEndpointAdapter{}.DecodeRequest(req)
	if failure != nil {
		t.Fatalf("DecodeRequest failure: %+v", failure)
	}
	if decoded.RequestedModel != "gpt-4o-mini-tts" || !decoded.Stream {
		t.Fatalf("decoded = %+v, want model gpt-4o-mini-tts and stream true", decoded)
	}
	defaultAudio := httptest.NewRequest(http.MethodPost, "/v1/audio/speech", strings.NewReader(`{"model":"tts-1","input":"speak this"}`))
	decodedDefault, defaultFailure := (audioSpeechEndpointAdapter{}).DecodeRequest(defaultAudio)
	if defaultFailure != nil || !decodedDefault.Stream {
		t.Fatalf("default audio request = %+v failure=%+v, want streaming transport", decodedDefault, defaultFailure)
	}
	explicitAudio := httptest.NewRequest(http.MethodPost, "/v1/audio/speech", strings.NewReader(`{"model":"tts-1","input":"speak this","stream_format":"audio"}`))
	decodedAudio, audioFailure := (audioSpeechEndpointAdapter{}).DecodeRequest(explicitAudio)
	if audioFailure != nil || !decodedAudio.Stream {
		t.Fatalf("explicit audio request = %+v failure=%+v, want streaming transport", decodedAudio, audioFailure)
	}
	for _, invalidBody := range []string{
		`{"model":"tts-1","input":"speak this","stream_format":"json"}`,
		`{"model":"tts-1","input":"speak this","stream_format":""}`,
		`{"model":"tts-1","input":"speak this","stream_format":true}`,
	} {
		invalid := httptest.NewRequest(http.MethodPost, "/v1/audio/speech", strings.NewReader(invalidBody))
		_, invalidFailure := (audioSpeechEndpointAdapter{}).DecodeRequest(invalid)
		if invalidFailure == nil || invalidFailure.status != http.StatusBadRequest || invalidFailure.code != "invalid_stream_format" {
			t.Fatalf("invalid stream_format failure = %+v for %s", invalidFailure, invalidBody)
		}
	}
	missingInput := httptest.NewRequest(http.MethodPost, "/v1/audio/speech", strings.NewReader(`{"model":"tts-1"}`))
	_, missingFailure := (audioSpeechEndpointAdapter{}).DecodeRequest(missingInput)
	if missingFailure == nil {
		t.Fatal("expected failure for missing input")
	}
}

func TestAudioSpeechUsesRawBufferedResponse(t *testing.T) {
	t.Parallel()

	if !protocolUsesRawBufferedResponse(openAIAudioSpeechProtocolAdapter{}) {
		t.Fatal("audio speech adapter must opt into raw buffered response (skip responses-stream buffer)")
	}
	if protocolUsesRawBufferedResponse(openAIEmbeddingsProtocolAdapter{}) {
		t.Fatal("embeddings adapter must not use raw buffered response")
	}
}

func TestParseAudioSpeechSSELineDetectsDone(t *testing.T) {
	t.Parallel()

	_, usage, done, ok := parseAudioSpeechSSELine([]byte(`data: {"type":"speech.audio.done","usage":{"input_tokens":5,"output_tokens":20}}`))
	if !ok || !done || usage == nil || usage.AudioOutputTokens != 20 {
		t.Fatalf("done+usage event = (usage=%v done=%v ok=%v), want done with usage", usage, done, ok)
	}

	_, _, doneNoUsage, ok := parseAudioSpeechSSELine([]byte(`data: {"type":"speech.audio.done"}`))
	if !ok || !doneNoUsage {
		t.Fatalf("done event without usage = (done=%v ok=%v), want done true regardless of usage", doneNoUsage, ok)
	}

	_, _, sentinelDone, ok := parseAudioSpeechSSELine([]byte(`data: [DONE]`))
	if !ok || !sentinelDone {
		t.Fatalf("[DONE] sentinel = (done=%v ok=%v), want done true", sentinelDone, ok)
	}

	chunk, _, done, ok := parseAudioSpeechSSELine([]byte(`data: {"type":"speech.audio.delta","audio":"QUJD"}`))
	if !ok || done || string(chunk) != "ABC" {
		t.Fatalf("delta event = (chunk=%q done=%v ok=%v), want ABC not-done", chunk, done, ok)
	}
}

func TestInspectAudioSpeechStreamLineCompletesOnDoneWithoutUsage(t *testing.T) {
	t.Parallel()

	capture := &streamCaptureState{}
	inspectAudioSpeechStreamLine([]byte(`data: {"type":"speech.audio.delta","audio":"QUJD"}`), capture)
	if capture.sawDone || capture.streamCompleted {
		t.Fatal("delta line must not complete the stream")
	}
	inspectAudioSpeechStreamLine([]byte(`data: {"type":"speech.audio.done"}`), capture)
	if !capture.sawDone || !capture.streamCompleted {
		t.Fatal("done event without usage must still mark the stream complete")
	}
}

func TestAudioSpeechProxyStreamConvertsSSEToAudio(t *testing.T) {
	t.Parallel()

	adapter := openAIAudioSpeechProtocolAdapter{sseMode: true, responseFormat: "mp3"}
	firstEvent := "data: {\"type\":\"speech.audio.delta\",\"audio\":\"" + base64.StdEncoding.EncodeToString([]byte("first-")) + "\"}\n\n"
	releaseSecond := make(chan struct{})
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: &stagedResponseBody{
			first:         firstEvent,
			second:        audioSSEBody([]string{"second"}, 7, 19),
			releaseSecond: releaseSecond,
		},
	}
	recorder := &audioSpeechStreamingRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		firstWrite:       make(chan struct{}),
	}
	type proxyResult struct {
		capture streamCaptureState
		started bool
		err     error
	}
	done := make(chan proxyResult, 1)
	go func() {
		capture, started, err := adapter.ProxyStream(context.Background(), recorder, response, time.Now(), routeengine.Candidate{})
		done <- proxyResult{capture: capture, started: started, err: err}
	}()
	select {
	case <-recorder.firstWrite:
	case <-time.After(time.Second):
		t.Fatal("first decoded audio chunk was not forwarded before the SSE stream completed")
	}
	if recorder.Body.String() != "first-" {
		t.Fatalf("body before SSE completion = %q, want first-", recorder.Body.String())
	}
	close(releaseSecond)
	result := <-done
	if result.err != nil || !result.started || !result.capture.streamCompleted || !result.capture.sawDone {
		t.Fatalf("ProxyStream result = %+v", result)
	}
	if recorder.Header().Get("Content-Type") != "audio/mpeg" || recorder.Body.String() != "first-second" {
		t.Fatalf("response content-type=%q body=%q", recorder.Header().Get("Content-Type"), recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "data:") {
		t.Fatalf("audio client received SSE framing: %q", recorder.Body.String())
	}
	if result.capture.usage.PromptTokens != 7 || result.capture.usage.AudioOutputTokens != 19 {
		t.Fatalf("usage = %+v, want input=7 audio_output=19", result.capture.usage)
	}
}

func TestAudioSpeechProxyStreamWritesRawAudioProgressively(t *testing.T) {
	t.Parallel()

	releaseSecond := make(chan struct{})
	body := &stagedResponseBody{first: "first", second: "second", releaseSecond: releaseSecond}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"audio/mpeg"}},
		Body:       body,
	}
	recorder := &audioSpeechStreamingRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		firstWrite:       make(chan struct{}),
	}
	adapter := openAIAudioSpeechProtocolAdapter{responseFormat: "mp3", inputChars: 12}
	type proxyResult struct {
		capture streamCaptureState
		started bool
		err     error
	}
	done := make(chan proxyResult, 1)
	go func() {
		capture, started, err := adapter.ProxyStream(context.Background(), recorder, response, time.Now(), routeengine.Candidate{})
		done <- proxyResult{capture: capture, started: started, err: err}
	}()

	select {
	case <-recorder.firstWrite:
	case <-time.After(time.Second):
		t.Fatal("first audio chunk was not forwarded before the upstream completed")
	}
	if recorder.Body.String() != "first" {
		t.Fatalf("body before upstream completion = %q, want first", recorder.Body.String())
	}
	close(releaseSecond)
	result := <-done
	if result.err != nil || !result.started || !result.capture.streamCompleted {
		t.Fatalf("ProxyStream result = %+v", result)
	}
	if recorder.Body.String() != "firstsecond" {
		t.Fatalf("final body = %q, want firstsecond", recorder.Body.String())
	}
	if result.capture.usage.PromptTokens != 12 {
		t.Fatalf("prompt tokens = %d, want 12", result.capture.usage.PromptTokens)
	}
}

func TestAudioSpeechExplicitSSERejectsBinaryUpstream(t *testing.T) {
	t.Parallel()

	adapter := openAIAudioSpeechProtocolAdapter{downstreamSSE: true}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"audio/mpeg"}},
		Body:       io.NopCloser(strings.NewReader("binary")),
	}
	recorder := httptest.NewRecorder()
	_, started, err := adapter.ProxyStream(context.Background(), recorder, response, time.Now(), routeengine.Candidate{})
	if err == nil || started || recorder.Body.Len() != 0 {
		t.Fatalf("binary upstream = started %v err %v body %q, want unstarted error", started, err, recorder.Body.String())
	}
}

func TestTransformBufferedResponseSSEModeFallsBackToBinaryOnNonSSEBody(t *testing.T) {
	t.Parallel()

	adapter := openAIAudioSpeechProtocolAdapter{sseMode: true, responseFormat: "mp3", inputChars: 5}
	headers := http.Header{}
	headers.Set("Content-Type", "audio/mpeg")

	transformed, err := adapter.TransformBufferedResponse(http.StatusOK, headers, []byte("raw-binary-not-sse"))
	if err != nil {
		t.Fatalf("TransformBufferedResponse error: %v", err)
	}
	if string(transformed.Body) != "raw-binary-not-sse" {
		t.Fatalf("body = %q, want binary passthrough (not empty SSE-collect)", string(transformed.Body))
	}
	if len(transformed.Body) == 0 {
		t.Fatal("must not emit an empty 200 when upstream returns non-SSE despite sse request")
	}
}

func TestBodyLooksLikeAudioSpeechSSE(t *testing.T) {
	t.Parallel()

	if !bodyLooksLikeAudioSpeechSSE([]byte("\n  data: {\"type\":\"speech.audio.delta\"}")) {
		t.Fatal("leading whitespace + data: should look like SSE")
	}
	if bodyLooksLikeAudioSpeechSSE([]byte(`{"error":{"message":"x"}}`)) {
		t.Fatal("JSON body must not look like SSE")
	}
	if bodyLooksLikeAudioSpeechSSE([]byte("binary-audio-bytes")) {
		t.Fatal("binary body must not look like SSE")
	}
}

type stagedResponseBody struct {
	first         string
	second        string
	releaseSecond <-chan struct{}
	readCount     int
}

func (b *stagedResponseBody) Read(p []byte) (int, error) {
	switch b.readCount {
	case 0:
		b.readCount++
		return copy(p, b.first), nil
	case 1:
		<-b.releaseSecond
		b.readCount++
		return copy(p, b.second), nil
	default:
		return 0, io.EOF
	}
}

func (*stagedResponseBody) Close() error {
	return nil
}

type audioSpeechStreamingRecorder struct {
	*httptest.ResponseRecorder
	firstWrite chan struct{}
	once       sync.Once
}

func (r *audioSpeechStreamingRecorder) Write(body []byte) (int, error) {
	written, err := r.ResponseRecorder.Write(body)
	r.once.Do(func() { close(r.firstWrite) })
	return written, err
}
