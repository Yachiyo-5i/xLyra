package gateway

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
)

type imageEditFilePart struct {
	filename    string
	contentType string
	data        []byte
}

func encodeOpenAIImagesEditsMultipart(payload map[string]any) ([]byte, string, bool) {
	refs := canonicalImageReferencesFromPayload(payload)
	if len(refs) == 0 {
		return nil, "", false
	}
	imageParts := make([]imageEditFilePart, 0, len(refs))
	for index, ref := range refs {
		part, ok := imageEditFilePartFromURL(ref.ImageURL, fmt.Sprintf("image-%d", index+1))
		if !ok {
			return nil, "", false
		}
		imageParts = append(imageParts, part)
	}
	var maskPart *imageEditFilePart
	if mask := canonicalImageMaskFromPayload(payload); mask != nil {
		part, ok := imageEditFilePartFromURL(mask.ImageURL, "mask")
		if !ok {
			return nil, "", false
		}
		maskPart = &part
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for key, value := range payload {
		if key == "image" || key == "images" || key == "mask" {
			continue
		}
		text, ok := multipartFieldValue(value)
		if !ok {
			continue
		}
		if err := writer.WriteField(key, text); err != nil {
			return nil, "", false
		}
	}
	for _, part := range imageParts {
		if err := writeImageEditFilePart(writer, "image", part); err != nil {
			return nil, "", false
		}
	}
	if maskPart != nil {
		if err := writeImageEditFilePart(writer, "mask", *maskPart); err != nil {
			return nil, "", false
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", false
	}
	return buf.Bytes(), writer.FormDataContentType(), true
}

func writeImageEditFilePart(writer *multipart.Writer, fieldName string, part imageEditFilePart) error {
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, fieldName, part.filename))
	header.Set("Content-Type", part.contentType)
	dest, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	_, err = dest.Write(part.data)
	return err
}

func imageEditFilePartFromURL(imageURL string, baseName string) (imageEditFilePart, bool) {
	data, contentType, ok := decodeImageDataURL(imageURL)
	if !ok {
		return imageEditFilePart{}, false
	}
	return imageEditFilePart{
		filename:    baseName + imageFileExtension(contentType),
		contentType: contentType,
		data:        data,
	}, true
}

func decodeImageDataURL(value string) ([]byte, string, bool) {
	text := strings.TrimSpace(value)
	if !strings.HasPrefix(strings.ToLower(text), "data:") {
		return nil, "", false
	}
	comma := strings.Index(text, ",")
	if comma < 0 {
		return nil, "", false
	}
	meta := strings.ToLower(text[len("data:"):comma])
	if !strings.Contains(meta, "base64") {
		return nil, "", false
	}
	encoded := text[comma+1:]
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		if data, err = base64.RawStdEncoding.DecodeString(encoded); err != nil {
			return nil, "", false
		}
	}
	contentType := strings.TrimSpace(strings.SplitN(meta, ";", 2)[0])
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = http.DetectContentType(data)
	}
	return data, contentType, true
}

func imageFileExtension(contentType string) string {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
}

func multipartFieldValue(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return "", false
		}
		return typed, true
	case bool:
		return strconv.FormatBool(typed), true
	case int:
		return strconv.Itoa(typed), true
	case int64:
		return strconv.FormatInt(typed, 10), true
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), true
	case json.Number:
		return typed.String(), true
	default:
		return "", false
	}
}
