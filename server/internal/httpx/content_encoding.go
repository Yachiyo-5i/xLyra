package httpx

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/klauspost/compress/zstd"
)

var ErrRequestBodyEncoding = errors.New("request body content encoding is invalid")

func DecompressRequestBody(maxBytes int64, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			encodingValues := r.Header.Values("Content-Encoding")
			encoding := ""
			if len(encodingValues) == 1 {
				encoding = strings.TrimSpace(encodingValues[0])
			}
			if encoding == "" || strings.EqualFold(encoding, "identity") {
				if len(encodingValues) > 1 {
					if r.Body != nil {
						_ = r.Body.Close()
					}
					Error(w, r, http.StatusUnsupportedMediaType, "unsupported_content_encoding", "unsupported content encoding")
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			if !strings.EqualFold(encoding, "zstd") {
				if r.Body != nil {
					_ = r.Body.Close()
				}
				Error(w, r, http.StatusUnsupportedMediaType, "unsupported_content_encoding", "unsupported content encoding")
				return
			}

			source := r.Body
			if source == nil {
				source = http.NoBody
			}
			if maxBytes > 0 && r.ContentLength > maxBytes {
				_ = source.Close()
				Error(w, r, http.StatusRequestEntityTooLarge, "request_body_too_large", "request body exceeds the maximum allowed size")
				return
			}
			compressed := &countingReader{reader: source, maxBytes: maxBytes}
			options := []zstd.DOption{zstd.WithDecoderConcurrency(1)}
			if maxBytes > 0 {
				decoderLimit := uint64(maxBytes)
				if decoderLimit < zstd.MinWindowSize {
					decoderLimit = zstd.MinWindowSize
				}
				options = append(options, zstd.WithDecoderMaxMemory(decoderLimit), zstd.WithDecoderMaxWindow(decoderLimit))
			}
			decoder, err := zstd.NewReader(compressed, options...)
			if err != nil {
				_ = source.Close()
				Error(w, r, http.StatusBadRequest, "invalid_content_encoding", "request body content encoding is invalid")
				return
			}

			body := &decompressedBody{
				decoder:    decoder,
				source:     source,
				compressed: compressed,
			}
			r.Body = body
			r.ContentLength = -1
			r.Header.Del("Content-Length")
			r.Header.Del("Content-Encoding")
			defer func() {
				_ = body.Close()
				if logger != nil {
					attrs := []any{
						"request_id", middleware.GetReqID(r.Context()),
						"endpoint", r.URL.Path,
						"content_encoding", "zstd",
						"compressed_bytes", body.compressedBytes(),
						"decompressed_bytes", body.decompressedBytes,
						"duration_ms", body.decodeDuration.Milliseconds(),
					}
					if body.readErr != nil {
						errorType := "zstd_decode_error"
						if errors.Is(body.readErr, ErrRequestBodyTooLarge) || errors.Is(body.readErr, zstd.ErrDecoderSizeExceeded) || errors.Is(body.readErr, zstd.ErrWindowSizeExceeded) {
							errorType = "request_body_too_large"
						}
						attrs = append(attrs, "error_type", errorType)
					}
					logger.DebugContext(r.Context(), "request body decompression", attrs...)
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

type countingReader struct {
	reader   io.Reader
	maxBytes int64
	bytes    int64
	exceeded bool
}

func (r *countingReader) Read(p []byte) (int, error) {
	if r.maxBytes > 0 {
		remaining := r.maxBytes - r.bytes
		if remaining <= 0 {
			r.exceeded = true
			return 0, ErrRequestBodyTooLarge
		}
		if int64(len(p)) > remaining {
			p = p[:remaining+1]
		}
	}
	n, err := r.reader.Read(p)
	r.bytes += int64(n)
	if r.maxBytes > 0 && r.bytes > r.maxBytes {
		r.exceeded = true
		return n, ErrRequestBodyTooLarge
	}
	return n, err
}

type decompressedBody struct {
	decoder           *zstd.Decoder
	source            io.Closer
	compressed        *countingReader
	decompressedBytes int64
	decodeDuration    time.Duration
	readErr           error
	closeOnce         sync.Once
}

func (b *decompressedBody) Read(p []byte) (int, error) {
	startedAt := time.Now()
	n, err := b.decoder.Read(p)
	b.decodeDuration += time.Since(startedAt)
	b.decompressedBytes += int64(n)
	if b.compressed.exceeded {
		b.readErr = ErrRequestBodyTooLarge
		return n, ErrRequestBodyTooLarge
	}
	if err != nil && err != io.EOF {
		b.readErr = err
		if errors.Is(err, ErrRequestBodyTooLarge) || errors.Is(err, zstd.ErrDecoderSizeExceeded) || errors.Is(err, zstd.ErrWindowSizeExceeded) {
			return n, ErrRequestBodyTooLarge
		}
		return n, contentEncodingReadError{err: err}
	}
	return n, err
}

func (b *decompressedBody) Close() error {
	b.closeOnce.Do(func() {
		b.decoder.Close()
		_ = b.source.Close()
	})
	return nil
}

func (b *decompressedBody) compressedBytes() int64 {
	return b.compressed.bytes
}

type contentEncodingReadError struct {
	err error
}

func (e contentEncodingReadError) Error() string {
	return ErrRequestBodyEncoding.Error()
}

func (e contentEncodingReadError) Is(target error) bool {
	return target == ErrRequestBodyEncoding || errors.Is(e.err, target)
}

func (e contentEncodingReadError) Unwrap() error {
	return e.err
}
