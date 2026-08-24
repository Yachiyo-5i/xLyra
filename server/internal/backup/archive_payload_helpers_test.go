package backup

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestParseTimeValueAcceptsBackupTimeEncodings(t *testing.T) {
	t.Parallel()

	when := time.Date(2026, 6, 22, 7, 8, 9, 123456789, time.UTC)
	tests := []struct {
		name  string
		value any
		want  time.Time
		ok    bool
	}{
		{name: "time", value: when, want: when, ok: true},
		{name: "zero time", value: time.Time{}},
		{name: "rfc3339 nano string", value: " " + when.Format(time.RFC3339Nano) + " ", want: when, ok: true},
		{name: "rfc3339 string", value: "2026-06-22T07:08:09Z", want: time.Date(2026, 6, 22, 7, 8, 9, 0, time.UTC), ok: true},
		{name: "blank string", value: "  "},
		{name: "invalid string", value: "2026/06/22"},
		{name: "unsupported type", value: 42},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := parseTimeValue(tt.value)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if ok && !got.Equal(tt.want) {
				t.Fatalf("time = %s, want %s", got.Format(time.RFC3339Nano), tt.want.Format(time.RFC3339Nano))
			}
		})
	}
}

func TestStringValueNormalizesBackupIdentifiers(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("00000000-0000-0000-0000-000000000501")
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "nil", value: nil, want: ""},
		{name: "trim string", value: "  api-key-1 \n", want: "api-key-1"},
		{name: "uuid", value: id, want: id.String()},
		{name: "nil uuid", value: uuid.Nil, want: ""},
		{name: "fallback", value: 17, want: "17"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := stringValue(tt.value); got != tt.want {
				t.Fatalf("stringValue(%#v) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestReferenceFiltersKeepExistingParentsAndClearInvalidNullableReferences(t *testing.T) {
	t.Parallel()

	parentID := uuid.MustParse("00000000-0000-0000-0000-000000000601")
	rows := []map[string]any{
		{"id": "valid-uuid", "parent_id": parentID},
		{"id": "valid-string", "parent_id": " parent-2 "},
		{"id": "missing", "parent_id": "missing-parent"},
		{"id": "blank", "parent_id": "  "},
		{"id": "nil"},
	}

	filtered := rowsWithExistingParent(rows, "parent_id", map[string]struct{}{
		parentID.String(): {},
		"parent-2":        {},
	})

	if len(filtered) != 2 {
		t.Fatalf("expected two rows with parents, got %#v", filtered)
	}
	if filtered[0]["id"] != "valid-uuid" || filtered[1]["id"] != "valid-string" {
		t.Fatalf("unexpected filtered rows: %#v", filtered)
	}

	nullableRows := []map[string]any{{
		"id":         "row-1",
		"site_id":    "site-1",
		"api_key_id": "missing",
	}, {
		"id":         "row-2",
		"site_id":    " ",
		"api_key_id": nil,
	}}
	got := rowsWithValidNullableReferences(nullableRows, map[string]map[string]struct{}{
		"site_id":    {"site-1": {}},
		"api_key_id": {"api-key-1": {}},
	})

	if got[0]["site_id"] != "site-1" {
		t.Fatalf("valid nullable reference was not preserved: %#v", got[0])
	}
	if got[0]["api_key_id"] != nil || got[1]["site_id"] != nil || got[1]["api_key_id"] != nil {
		t.Fatalf("invalid nullable references were not cleared: %#v", got)
	}
}

func TestRowValuePreservesJSONNumbersAndStringFallbacks(t *testing.T) {
	t.Parallel()

	jsonValue := rowValue(map[string]any{"count": json.Number("2")}, true)
	if jsonValue != `{"count":2}` {
		t.Fatalf("unexpected JSON row value: %#v", jsonValue)
	}
	if got := rowValue(func() {}, true); got != "null" {
		t.Fatalf("expected marshal failure to fall back to null, got %#v", got)
	}
	if got := rowValue(map[string]any{"name": "daily"}, false); got != `{"name":"daily"}` {
		t.Fatalf("unexpected map row value: %#v", got)
	}
	if got := rowValue([]any{"a", json.Number("3")}, false); got != `["a",3]` {
		t.Fatalf("unexpected slice row value: %#v", got)
	}
	if got := rowValue(json.Number("42"), false); got != int64(42) {
		t.Fatalf("expected integer json.Number conversion, got %#v", got)
	}
	if got := rowValue(json.Number("4.25"), false); got != 4.25 {
		t.Fatalf("expected float json.Number conversion, got %#v", got)
	}
	if got := rowValue(json.Number("not-a-number"), false); got != "not-a-number" {
		t.Fatalf("expected invalid json.Number to stay string, got %#v", got)
	}
}

func TestArchivePayloadDecodersPreserveNumbersAndReportMalformedEntries(t *testing.T) {
	t.Parallel()

	files := archivePayloadZipFiles(t, map[string]string{
		"manifest.json":              `{"count":9007199254740993}`,
		"bad.json":                   `{`,
		"database/sites.jsonl":       "{\"id\":\"site-1\",\"priority\":1}\n{\"id\":\"site-2\"}\n",
		"database/malformed.jsonl":   "{\"id\":\"ok\"}\n{",
		"database/empty_table.jsonl": "",
	})

	if file := archiveFile(files, "manifest.json"); file == nil {
		t.Fatal("expected archive file to be found")
	}
	if file := archiveFile(files, "missing.json"); file != nil {
		t.Fatalf("expected missing archive file, got %#v", file.Name)
	}

	var decoded map[string]any
	if err := decodeArchiveJSON(files, "manifest.json", &decoded); err != nil {
		t.Fatalf("decode archive json: %v", err)
	}
	if decoded["count"] != json.Number("9007199254740993") {
		t.Fatalf("expected json.Number to be preserved, got %#v", decoded)
	}
	if err := decodeArchiveJSON(files, "missing.json", &decoded); err == nil || !strings.Contains(err.Error(), "missing missing.json") {
		t.Fatalf("expected missing JSON error, got %v", err)
	}
	if err := decodeArchiveJSON(files, "bad.json", &decoded); err == nil || !strings.Contains(err.Error(), "decode bad.json") {
		t.Fatalf("expected malformed JSON error, got %v", err)
	}

	rows, err := decodeArchiveTable(files, "sites", true)
	if err != nil {
		t.Fatalf("decode archive table: %v", err)
	}
	if len(rows) != 2 || rows[0]["id"] != "site-1" || rows[0]["priority"] != json.Number("1") {
		t.Fatalf("unexpected decoded rows: %#v", rows)
	}
	emptyRows, err := decodeArchiveTable(files, "empty_table", true)
	if err != nil {
		t.Fatalf("decode empty archive table: %v", err)
	}
	if len(emptyRows) != 0 {
		t.Fatalf("expected empty table, got %#v", emptyRows)
	}
	missingRows, err := decodeArchiveTable(files, "missing_table", false)
	if err != nil {
		t.Fatalf("expected missing table to decode as empty, got %v", err)
	}
	if len(missingRows) != 0 {
		t.Fatalf("expected missing table to decode as empty, got %#v", missingRows)
	}
	if _, err := decodeArchiveTable(files, "malformed", true); err == nil || !strings.Contains(err.Error(), "decode database/malformed.jsonl") {
		t.Fatalf("expected malformed table error, got %v", err)
	}
}

func TestEnvelopeHeaderRoundTripAndValidationErrors(t *testing.T) {
	t.Parallel()

	salt := []byte("1234567890abcdef")
	noncePrefix := []byte("nonce123")
	var buf bytes.Buffer
	if err := writeEnvelopeHeader(&buf, salt, noncePrefix); err != nil {
		t.Fatalf("write envelope header: %v", err)
	}
	header, err := readEnvelopeHeader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("read envelope header: %v", err)
	}
	if !bytes.Equal(header.salt, salt) || !bytes.Equal(header.noncePrefix, noncePrefix) {
		t.Fatalf("unexpected header: %#v", header)
	}

	if _, err := readEnvelopeHeader(strings.NewReader("wrong")); err == nil || !strings.Contains(err.Error(), "read backup header") {
		t.Fatalf("expected short magic read error, got %v", err)
	}
	if _, err := readEnvelopeHeader(bytes.NewReader(envelopeHeaderBytes(1, salt, noncePrefix))); err == nil || !strings.Contains(err.Error(), "unsupported backup format version 1") {
		t.Fatalf("expected version error, got %v", err)
	}
	if _, err := readEnvelopeHeader(bytes.NewReader(append([]byte(envelopeMagic), 0, byte(envelopeVersion)))); err == nil || !strings.Contains(err.Error(), "read backup header") {
		t.Fatalf("expected truncated salt error, got %v", err)
	}
	if err := writeEnvelopeHeader(closedPipeWriter{}, salt, noncePrefix); err == nil || !strings.Contains(err.Error(), "write backup header") {
		t.Fatalf("expected header writer error, got %v", err)
	}
}

func TestEncryptedChunkRoundTripAndIOErrors(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := writeChunk(&buf, []byte("ciphertext")); err != nil {
		t.Fatalf("write chunk: %v", err)
	}
	if err := writeChunk(&buf, nil); err != nil {
		t.Fatalf("write terminal chunk: %v", err)
	}

	chunk, err := readChunk(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("read chunk: %v", err)
	}
	if !bytes.Equal(chunk, []byte("ciphertext")) {
		t.Fatalf("unexpected chunk: %q", chunk)
	}
	terminator, err := readChunk(bytes.NewReader([]byte{0, 0, 0, 0}))
	if err != nil {
		t.Fatalf("read terminal chunk: %v", err)
	}
	if terminator != nil {
		t.Fatalf("expected nil terminal chunk, got %#v", terminator)
	}

	var truncated bytes.Buffer
	if err := binary.Write(&truncated, binary.BigEndian, uint32(4)); err != nil {
		t.Fatalf("write truncated chunk length: %v", err)
	}
	truncated.WriteString("xy")
	if _, err := readChunk(bytes.NewReader(truncated.Bytes())); err == nil || !strings.Contains(err.Error(), "read backup chunk") {
		t.Fatalf("expected truncated chunk error, got %v", err)
	}
	if err := writeChunk(closedPipeWriter{}, []byte("ciphertext")); err == nil || !strings.Contains(err.Error(), "write backup chunk") {
		t.Fatalf("expected chunk length writer error, got %v", err)
	}
	payloadFail := &limitedChunkWriter{limit: 4}
	if err := writeChunk(payloadFail, []byte("ciphertext")); err == nil || !strings.Contains(err.Error(), "write backup chunk") {
		t.Fatalf("expected chunk payload writer error, got %v", err)
	}
}

func TestChunkNonceCombinesPrefixAndIndexWithoutAliasing(t *testing.T) {
	t.Parallel()

	prefix := []byte("12345678")
	nonce := chunkNonce(prefix, 7)
	want := append([]byte("12345678"), 0, 0, 0, 7)
	if !reflect.DeepEqual(nonce, want) {
		t.Fatalf("nonce = %#v, want %#v", nonce, want)
	}
	prefix[0] = 'x'
	if string(nonce[:8]) != "12345678" {
		t.Fatalf("nonce aliased source prefix: %#v", nonce)
	}
}

func archivePayloadZipFiles(t *testing.T, entries map[string]string) []*zip.File {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close archive payload zip: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("open archive payload zip: %v", err)
	}
	return zr.File
}

func envelopeHeaderBytes(version uint16, salt []byte, noncePrefix []byte) []byte {
	var buf bytes.Buffer
	buf.WriteString(envelopeMagic)
	_ = binary.Write(&buf, binary.BigEndian, version)
	buf.Write(salt)
	buf.Write(noncePrefix)
	return buf.Bytes()
}

type closedPipeWriter struct{}

func (closedPipeWriter) Write([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}

type limitedChunkWriter struct {
	limit int
	wrote int
}

func (w *limitedChunkWriter) Write(p []byte) (int, error) {
	remaining := w.limit - w.wrote
	if remaining <= 0 {
		return 0, io.ErrClosedPipe
	}
	if len(p) > remaining {
		w.wrote += remaining
		return remaining, io.ErrClosedPipe
	}
	w.wrote += len(p)
	return len(p), nil
}
