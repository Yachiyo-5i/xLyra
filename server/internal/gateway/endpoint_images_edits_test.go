package gateway

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	routeengine "xlyra/server/internal/router"
)

func TestImagesEditsEndpointAdapterDecodesJSONRequest(t *testing.T) {
	t.Parallel()

	request := requireDecodedEndpointRequest(t, imagesEditsEndpointAdapter{}, `{"model":"gpt-image-2","prompt":"edit this cat","images":[{"image_url":"data:image/png;base64,aWZha2U="}]}`, gatewayEndpointImagesEdits, "gpt-image-2")
	if request.Stream {
		t.Fatal("expected stream=false")
	}
	if request.Canonical == nil || request.Canonical.Image == nil {
		t.Fatal("expected canonical image request")
	}
	if request.Canonical.Image.Action != "edit" {
		t.Fatalf("Action = %q, want edit", request.Canonical.Image.Action)
	}
}

func TestImagesEditsEndpointAdapterDefaultsModel(t *testing.T) {
	t.Parallel()

	request := requireDecodedEndpointRequest(t, imagesEditsEndpointAdapter{}, `{"prompt":"edit this cat"}`, gatewayEndpointImagesEdits, defaultImageEditModel)
	if request.RequestedModel != defaultImageEditModel {
		t.Fatalf("RequestedModel = %q, want %q", request.RequestedModel, defaultImageEditModel)
	}
}

func TestImagesEditsEndpointAdapterDecodesMultipart(t *testing.T) {
	t.Parallel()

	body, contentType := buildMultipartImageRequest(t, map[string]string{
		"prompt": "edit this cat",
		"model":  "gpt-image-2",
	}, map[string][]byte{
		"image": {0x89, 0x50, 0x4E, 0x47},
	})

	req := httptest.NewRequest(http.MethodPost, gatewayEndpointImagesEdits, body)
	req.Header.Set("Content-Type", contentType)

	request, failure := imagesEditsEndpointAdapter{}.DecodeRequest(req)
	if failure != nil {
		t.Fatalf("DecodeRequest returned failure: %+v", failure)
	}
	if request.DownstreamPath != gatewayEndpointImagesEdits {
		t.Fatalf("DownstreamPath = %q, want %q", request.DownstreamPath, gatewayEndpointImagesEdits)
	}
	if request.Canonical == nil || request.Canonical.Image == nil {
		t.Fatal("expected canonical image request")
	}
	if len(request.Canonical.Image.Images) != 1 {
		t.Fatalf("Images count = %d, want 1", len(request.Canonical.Image.Images))
	}
	if !strings.HasPrefix(request.Canonical.Image.Images[0].ImageURL, "data:") {
		t.Fatalf("expected data URL, got %q", request.Canonical.Image.Images[0].ImageURL)
	}
}

func TestImagesEditsEndpointAdapterDecodesMultipartWithMask(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("prompt", "edit this")
	_ = writer.WriteField("model", "gpt-image-2")
	imgPart, _ := writer.CreateFormFile("image", "image.png")
	_, _ = imgPart.Write([]byte{0x89, 0x50, 0x4E, 0x47})
	maskPart, _ := writer.CreateFormFile("mask", "mask.png")
	_, _ = maskPart.Write([]byte{0x89, 0x50, 0x4E, 0x47})
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, gatewayEndpointImagesEdits, &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	request, failure := imagesEditsEndpointAdapter{}.DecodeRequest(req)
	if failure != nil {
		t.Fatalf("DecodeRequest returned failure: %+v", failure)
	}
	if request.Canonical.Image.Mask == nil {
		t.Fatal("expected mask to be parsed")
	}
	if !strings.HasPrefix(request.Canonical.Image.Mask.ImageURL, "data:") {
		t.Fatalf("expected data URL for mask, got %q", request.Canonical.Image.Mask.ImageURL)
	}
}

func TestImagesEditsRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	assertEndpointDecodeFailure(t, "invalid JSON", imagesEditsEndpointAdapter{}, `{`, "invalid_json", "decode")
}

func TestImagesEditsCodexBuildEditsPayload(t *testing.T) {
	t.Parallel()

	protocol := newCodexProtocolAdapter(gatewayRequest{DownstreamPath: gatewayEndpointImagesEdits})
	canonical := canonicalRequestFromOpenAIImagesPayloadForPath(map[string]any{
		"model":  "gpt-image-2",
		"prompt": "edit this cat",
		"images": []any{map[string]any{"image_url": "data:image/png;base64,aWZha2U="}},
		"mask":   map[string]any{"image_url": "data:image/png;base64,bWFzaw=="},
		"n":      2,
		"size":   "1024x1024",
	}, "gpt-image-2", gatewayEndpointImagesEdits)

	payload, err := protocol.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointImagesEdits,
		Canonical:      &canonical,
	}, routeengine.Candidate{
		Model: routeengine.CandidateModel{UpstreamName: "gpt-image-2"},
	})
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}

	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want one tool", payload["tools"])
	}
	tool, _ := tools[0].(map[string]any)
	if got := tool["action"]; got != "edit" {
		t.Fatalf("tool.action = %#v, want edit", got)
	}
	if got := tool["n"]; got != 2 {
		t.Fatalf("tool.n = %#v, want 2", got)
	}
	mask, _ := tool["input_image_mask"].(map[string]any)
	if mask == nil || mask["image_url"] != "data:image/png;base64,bWFzaw==" {
		t.Fatalf("tool.input_image_mask = %#v", tool["input_image_mask"])
	}

	input, _ := payload["input"].([]any)
	if len(input) != 1 {
		t.Fatalf("input count = %d, want 1", len(input))
	}
	msg, _ := input[0].(map[string]any)
	content, _ := msg["content"].([]any)
	if len(content) < 2 {
		t.Fatalf("content count = %d, want >= 2 (text + image)", len(content))
	}
	imgPart, _ := content[1].(map[string]any)
	if imgPart["type"] != "input_image" {
		t.Fatalf("content[1].type = %#v, want input_image", imgPart["type"])
	}
}

func TestImagesEditsCodexStreamPrefixIsImageEdit(t *testing.T) {
	t.Parallel()

	adapter := newCodexProtocolAdapter(gatewayRequest{DownstreamPath: gatewayEndpointImagesEdits})

	resp := gatewayStreamTestResponse(
		`data: {"type":"response.completed","response":{"id":"resp_123","output":[{"type":"image_generation_call","result":"ZmFrZQ=="}],"usage":{"input_tokens":5,"output_tokens":9,"total_tokens":14}}}` + "\n\n",
	)
	rec := httptest.NewRecorder()

	capture, started, err := adapter.ProxyStream(t.Context(), rec, resp, time.Now(), routeengine.Candidate{})
	if err != nil {
		t.Fatalf("ProxyStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted {
		t.Fatalf("started=%v streamCompleted=%v", started, capture.streamCompleted)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: image_edit.completed") {
		t.Fatalf("expected event prefix image_edit, got body: %s", body)
	}
	if strings.Contains(body, "image_generation.completed") {
		t.Fatal("should not contain image_generation prefix for edits endpoint")
	}
}

func TestImagesEditsEndpointAdapterDecodesStandardImageField(t *testing.T) {
	t.Parallel()

	request := requireDecodedEndpointRequest(t, imagesEditsEndpointAdapter{}, `{"model":"gpt-image-2","prompt":"edit this cat","image":["data:image/png;base64,aWZha2U=","aWZha2U="]}`, gatewayEndpointImagesEdits, "gpt-image-2")
	if request.Canonical == nil || request.Canonical.Image == nil {
		t.Fatal("expected canonical image request")
	}
	images := request.Canonical.Image.Images
	if len(images) != 2 {
		t.Fatalf("Images count = %d, want 2", len(images))
	}
	if images[0].ImageURL != "data:image/png;base64,aWZha2U=" {
		t.Fatalf("Images[0] = %q", images[0].ImageURL)
	}
	if images[1].ImageURL != "data:image/png;base64,aWZha2U=" {
		t.Fatalf("Images[1] = %q, want bare base64 wrapped as data URL", images[1].ImageURL)
	}
}

func TestOpenAIImagesEditsBuildsMultipartUpstreamBody(t *testing.T) {
	t.Parallel()

	imageBytes := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	maskBytes := []byte("maskdata")
	payload := map[string]any{
		"model":  "gpt-image-2",
		"prompt": "edit this cat",
		"n":      2,
		"size":   "1024x1024",
		"stream": false,
		"images": []any{map[string]any{"image_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageBytes)}},
		"mask":   map[string]any{"image_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(maskBytes)},
	}

	adapter := newOpenAIImagesProtocolAdapter(gatewayRequest{DownstreamPath: gatewayEndpointImagesEdits})
	body, contentType, err := adapter.BuildUpstreamBody(gatewayRequest{DownstreamPath: gatewayEndpointImagesEdits}, payload)
	if err != nil {
		t.Fatalf("BuildUpstreamBody returned error: %v", err)
	}

	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" {
		t.Fatalf("contentType = %q (mediaType=%q, err=%v), want multipart/form-data", contentType, mediaType, err)
	}

	form, err := multipart.NewReader(bytes.NewReader(body), params["boundary"]).ReadForm(1 << 20)
	if err != nil {
		t.Fatalf("ReadForm returned error: %v", err)
	}
	defer form.RemoveAll()

	for field, want := range map[string]string{
		"model":  "gpt-image-2",
		"prompt": "edit this cat",
		"n":      "2",
		"size":   "1024x1024",
		"stream": "false",
	} {
		if got := form.Value[field]; len(got) != 1 || got[0] != want {
			t.Fatalf("form field %q = %#v, want %q", field, got, want)
		}
	}
	if _, exists := form.Value["images"]; exists {
		t.Fatal("form should not contain internal images field")
	}

	imageFiles := form.File["image"]
	if len(imageFiles) != 1 {
		t.Fatalf("image file count = %d, want 1", len(imageFiles))
	}
	imageFile, err := imageFiles[0].Open()
	if err != nil {
		t.Fatalf("open image file: %v", err)
	}
	defer imageFile.Close()
	uploaded := make([]byte, len(imageBytes)+1)
	read, _ := imageFile.Read(uploaded)
	if !bytes.Equal(uploaded[:read], imageBytes) {
		t.Fatalf("image bytes = %v, want %v", uploaded[:read], imageBytes)
	}
	if got := imageFiles[0].Header.Get("Content-Type"); got != "image/png" {
		t.Fatalf("image part content type = %q, want image/png", got)
	}

	if len(form.File["mask"]) != 1 {
		t.Fatalf("mask file count = %d, want 1", len(form.File["mask"]))
	}
}

func TestOpenAIImagesEditsFallsBackToJSONForRemoteImage(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"model":  "gpt-image-2",
		"prompt": "edit this cat",
		"images": []any{map[string]any{"image_url": "https://example.com/cat.png"}},
	}

	adapter := newOpenAIImagesProtocolAdapter(gatewayRequest{DownstreamPath: gatewayEndpointImagesEdits})
	body, contentType, err := adapter.BuildUpstreamBody(gatewayRequest{DownstreamPath: gatewayEndpointImagesEdits}, payload)
	if err != nil {
		t.Fatalf("BuildUpstreamBody returned error: %v", err)
	}
	if contentType != "application/json" {
		t.Fatalf("contentType = %q, want application/json", contentType)
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if _, ok := decoded["images"]; !ok {
		t.Fatal("expected images field preserved in JSON fallback")
	}
}

func TestOpenAIImagesEditsUpstreamPath(t *testing.T) {
	t.Parallel()

	adapter := newOpenAIImagesProtocolAdapter(gatewayRequest{DownstreamPath: gatewayEndpointImagesEdits})
	path := adapter.UpstreamPath("https://api.openai.com")
	if !strings.HasSuffix(path, "/v1/images/edits") {
		t.Fatalf("UpstreamPath = %q, want suffix /v1/images/edits", path)
	}

	generationsAdapter := newOpenAIImagesProtocolAdapter(gatewayRequest{DownstreamPath: gatewayEndpointImagesGenerations})
	generationsPath := generationsAdapter.UpstreamPath("https://api.openai.com")
	if !strings.HasSuffix(generationsPath, "/v1/images/generations") {
		t.Fatalf("UpstreamPath = %q, want suffix /v1/images/generations", generationsPath)
	}
}

func buildMultipartImageRequest(t *testing.T, fields map[string]string, files map[string][]byte) (*bytes.Buffer, string) {
	t.Helper()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for key, value := range fields {
		_ = writer.WriteField(key, value)
	}
	for fieldName, data := range files {
		part, err := writer.CreateFormFile(fieldName, fieldName+".png")
		if err != nil {
			t.Fatalf("CreateFormFile returned error: %v", err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatalf("Write returned error: %v", err)
		}
	}
	_ = writer.Close()
	return &buf, writer.FormDataContentType()
}
