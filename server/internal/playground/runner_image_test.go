package playground

import (
	"encoding/json"
	"io"
	"testing"
)

func TestImageGatewayBodyUsesImageGenerationsRouteAndSelectedModel(t *testing.T) {
	service := &Service{}
	payload := RunPayload{
		Mode:  ModeImage,
		Model: "gpt-image-2",
		Image: &ImageConversation{Entries: []ImageEntry{{Prompt: "draw a cat", Size: "1024x1024"}}},
	}

	body, contentType, path, err := service.imageGatewayBody(t.Context(), payload)
	if err != nil {
		t.Fatalf("imageGatewayBody() error = %v", err)
	}
	if path != "/images/generations" {
		t.Fatalf("image path = %q, want /images/generations", path)
	}
	if contentType != "application/json" {
		t.Fatalf("image content type = %q, want application/json", contentType)
	}
	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read image body: %v", err)
	}
	var request map[string]any
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatalf("decode image body: %v", err)
	}
	if request["model"] != "gpt-image-2" {
		t.Fatalf("image model = %#v, want gpt-image-2", request["model"])
	}
}
