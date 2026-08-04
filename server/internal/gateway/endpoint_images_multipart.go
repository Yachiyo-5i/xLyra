package gateway

import (
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
)

const maxImageUploadBytes = 25 << 20

func isMultipartRequest(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/")
}

func decodeImageMultipartPayload(r *http.Request) (map[string]any, *chatFailure) {
	if err := r.ParseMultipartForm(maxImageUploadBytes); err != nil {
		return nil, &chatFailure{
			status:  http.StatusBadRequest,
			code:    "invalid_multipart",
			message: "failed to parse multipart form data",
			stage:   "decode",
		}
	}

	payload := map[string]any{}

	for _, key := range []string{
		"model", "prompt", "size", "quality", "background",
		"output_format", "moderation", "input_fidelity", "style",
		"response_format",
	} {
		if value := strings.TrimSpace(r.FormValue(key)); value != "" {
			payload[key] = value
		}
	}

	for _, key := range []string{"n", "output_compression", "partial_images"} {
		if raw := strings.TrimSpace(r.FormValue(key)); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil {
				payload[key] = v
			}
		}
	}

	if raw := strings.TrimSpace(r.FormValue("stream")); raw != "" {
		payload["stream"] = raw == "true" || raw == "1"
	}

	images, failure := readMultipartImageFiles(r)
	if failure != nil {
		return nil, failure
	}
	if len(images) > 0 {
		refs := make([]any, 0, len(images))
		for _, dataURL := range images {
			refs = append(refs, map[string]any{"image_url": dataURL})
		}
		payload["images"] = refs
	}

	maskDataURL, failure := readMultipartSingleFile(r, "mask")
	if failure != nil {
		return nil, failure
	}
	if maskDataURL != "" {
		payload["mask"] = map[string]any{"image_url": maskDataURL}
	}

	return payload, nil
}

func readMultipartImageFiles(r *http.Request) ([]string, *chatFailure) {
	var dataURLs []string

	if fh := r.MultipartForm.File["image"]; len(fh) > 0 {
		for _, header := range fh {
			dataURL, failure := readFileHeaderAsDataURL(header)
			if failure != nil {
				return nil, failure
			}
			dataURLs = append(dataURLs, dataURL)
		}
		return dataURLs, nil
	}

	for i := 0; ; i++ {
		key := fmt.Sprintf("image[%d]", i)
		fh := r.MultipartForm.File[key]
		if len(fh) == 0 {
			break
		}
		dataURL, failure := readFileHeaderAsDataURL(fh[0])
		if failure != nil {
			return nil, failure
		}
		dataURLs = append(dataURLs, dataURL)
	}

	return dataURLs, nil
}

func readMultipartSingleFile(r *http.Request, fieldName string) (string, *chatFailure) {
	fh := r.MultipartForm.File[fieldName]
	if len(fh) == 0 {
		return "", nil
	}
	return readFileHeaderAsDataURL(fh[0])
}

func readFileHeaderAsDataURL(fh *multipart.FileHeader) (string, *chatFailure) {
	file, err := fh.Open()
	if err != nil {
		return "", &chatFailure{
			status:  http.StatusBadRequest,
			code:    "invalid_file",
			message: "failed to read uploaded file",
			stage:   "decode",
		}
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxImageUploadBytes))
	if err != nil {
		return "", &chatFailure{
			status:  http.StatusBadRequest,
			code:    "invalid_file",
			message: "failed to read uploaded file data",
			stage:   "decode",
		}
	}

	contentType := strings.TrimSpace(fh.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	if contentType == "application/octet-stream" {
		contentType = "image/png"
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return "data:" + contentType + ";base64," + encoded, nil
}
