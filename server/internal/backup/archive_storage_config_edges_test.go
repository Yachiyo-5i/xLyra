package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"xlyra/server/internal/config"
)

func TestWriteArchiveReportsCloseZipError(t *testing.T) {
	t.Parallel()

	payload := archivePayload{
		Manifest: manifest{
			FormatVersion: currentFormatVersion,
			App:           backupAppName,
			Payload:       backupPayload,
			CreatedAt:     time.Date(2026, 6, 23, 1, 2, 3, 0, time.UTC),
			Tables:        append([]string(nil), exportTables...),
		},
		Config: map[string]any{"general": map[string]any{"log": map[string]any{"level": "info"}}},
	}

	err := writeArchive(payload, func(*zip.Writer) error { return nil }, &zipCentralDirectoryFailWriter{})
	if err == nil || !strings.Contains(err.Error(), "close zip") {
		t.Fatalf("writeArchive error = %v, want close zip context", err)
	}
}

func TestNormalizeS3EndpointRejectsMissingURLHost(t *testing.T) {
	t.Parallel()

	endpoint, secure, err := normalizeS3Endpoint("https:///bucket", false)
	if err == nil || !strings.Contains(err.Error(), "storage.endpoint host is required") {
		t.Fatalf("normalizeS3Endpoint endpoint=%q secure=%v err=%v, want host validation", endpoint, secure, err)
	}
}

func TestListFilesWrapsRoundTripperError(t *testing.T) {
	t.Parallel()

	service := NewAutomaticService(Service{}, "master-key")
	cfg := automaticS3TestConfig()
	client := automaticS3TestClient(t, failingRoundTripper{err: errors.New("network unavailable")})

	files, err := service.listFilesWithClient(context.Background(), cfg, client, 10)
	if err == nil || !strings.Contains(err.Error(), "list backup files from S3") || !strings.Contains(err.Error(), "network unavailable") {
		t.Fatalf("listFilesWithClient files=%#v err=%v, want wrapped transport error", files, err)
	}
}

func TestUpdateConfigRejectsRetentionAboveLimit(t *testing.T) {
	t.Parallel()

	confFile, err := config.LoadConfigFile(t.TempDir())
	if err != nil {
		t.Fatalf("load config file: %v", err)
	}
	service := NewAutomaticService(Service{confFile: confFile}, "master-key")
	payload, err := service.UpdateConfig(AutomaticConfigInput{
		Cron:             config.DefaultAutomaticBackupConfig().Cron,
		RetentionCount:   366,
		Endpoint:         "s3.example.com",
		Bucket:           "xlyra",
		AccessKey:        "access",
		SecretKey:        "secret",
		BackupPassphrase: "passphrase",
	})
	if err == nil || !strings.Contains(err.Error(), "retention_count must be less than or equal to 365") {
		t.Fatalf("UpdateConfig payload=%#v err=%v, want retention upper bound validation", payload, err)
	}
}

type zipCentralDirectoryFailWriter struct {
	buf bytes.Buffer
}

func (w *zipCentralDirectoryFailWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("PK\x01\x02")) || bytes.Contains(p, []byte("PK\x05\x06")) {
		return 0, errors.New("central directory write failed")
	}
	return w.buf.Write(p)
}

type failingRoundTripper struct {
	err error
}

func (rt failingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, rt.err
}
