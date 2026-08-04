package backup

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"xlyra/server/internal/config"
)

func TestAutomaticListFilesWithClientFiltersSortsAndLimits(t *testing.T) {
	t.Parallel()

	objects := []automaticS3TestObject{
		{Key: "prod/notes.txt", Size: 99, LastModified: "2026-06-23T01:00:00.000Z"},
		{Key: "other/xlyra-backup-20260623-010000.zip.xlyra", Size: 23, LastModified: "2026-06-23T01:00:00.000Z"},
		{Key: "prod/xlyra-backup-20260622-010000.zip.xlyra", Size: 22, LastModified: "2026-06-22T01:00:00.000Z"},
		{Key: "prod/xlyra-backup-20260621-010000.zip.xlyra", Size: 21, LastModified: "2026-06-21T01:00:00.000Z"},
	}
	service := NewAutomaticService(Service{}, "master-key")
	cfg := automaticS3TestConfig()
	client := automaticS3TestClient(t, automaticS3TestTransport(t, objects, nil))

	files, err := service.listFilesWithClient(context.Background(), cfg, client, 1)
	if err != nil {
		t.Fatalf("listFilesWithClient: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("expected limit to keep one backup file, got %#v", files)
	}
	if files[0].Key != "prod/xlyra-backup-20260622-010000.zip.xlyra" || files[0].Filename != "xlyra-backup-20260622-010000.zip.xlyra" || files[0].Size != 22 {
		t.Fatalf("unexpected listed file: %#v", files[0])
	}
}

func TestAutomaticPruneBackupsDeletesOnlyFilesPastRetention(t *testing.T) {
	t.Parallel()

	transport := automaticS3TestTransport(t, []automaticS3TestObject{
		{Key: "prod/xlyra-backup-20260623-010000.zip.xlyra", Size: 23, LastModified: "2026-06-23T01:00:00.000Z"},
		{Key: "prod/xlyra-backup-20260622-010000.zip.xlyra", Size: 22, LastModified: "2026-06-22T01:00:00.000Z"},
		{Key: "prod/xlyra-backup-20260621-010000.zip.xlyra", Size: 21, LastModified: "2026-06-21T01:00:00.000Z"},
	}, nil)
	service := NewAutomaticService(Service{}, "master-key")
	cfg := automaticS3TestConfig()
	cfg.RetentionCount = 1
	client := automaticS3TestClient(t, transport)

	deleted, err := service.pruneBackups(context.Background(), cfg, client)
	if err != nil {
		t.Fatalf("pruneBackups: %v", err)
	}

	want := []string{
		"prod/xlyra-backup-20260622-010000.zip.xlyra",
		"prod/xlyra-backup-20260621-010000.zip.xlyra",
	}
	if strings.Join(deleted, ",") != strings.Join(want, ",") {
		t.Fatalf("deleted = %#v, want %#v", deleted, want)
	}
	if got := transport.deletedKeys(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("server deleted keys = %#v, want %#v", got, want)
	}
}

func TestAutomaticListAndPrunePropagateStorageErrors(t *testing.T) {
	t.Parallel()

	service := NewAutomaticService(Service{}, "master-key")
	cfg := automaticS3TestConfig()
	client := automaticS3TestClient(t, automaticS3TestTransport(t, nil, map[string]int{http.MethodGet: http.StatusInternalServerError}))
	if _, err := service.listFilesWithClient(context.Background(), cfg, client, automaticBackupListLimit); err == nil || !strings.Contains(err.Error(), "list backup files from S3") {
		t.Fatalf("listFilesWithClient error = %v, want storage list context", err)
	}

	transport := automaticS3TestTransport(t, []automaticS3TestObject{
		{Key: "prod/xlyra-backup-20260623-010000.zip.xlyra", Size: 23, LastModified: "2026-06-23T01:00:00.000Z"},
		{Key: "prod/xlyra-backup-20260622-010000.zip.xlyra", Size: 22, LastModified: "2026-06-22T01:00:00.000Z"},
	}, map[string]int{http.MethodDelete: http.StatusInternalServerError})
	cfg.RetentionCount = 1
	client = automaticS3TestClient(t, transport)
	deleted, err := service.pruneBackups(context.Background(), cfg, client)
	if err == nil || !strings.Contains(err.Error(), "prune backup prod/xlyra-backup-20260622-010000.zip.xlyra") {
		t.Fatalf("pruneBackups error = %v, deleted=%#v; want delete context", err, deleted)
	}
	if len(deleted) != 0 {
		t.Fatalf("deleted before first failed remove = %#v, want none", deleted)
	}
}

type automaticS3TestObject struct {
	Key          string
	Size         int64
	LastModified string
}

type automaticS3TestRoundTripper struct {
	t       *testing.T
	objects []automaticS3TestObject
	status  map[string]int
	mu      sync.Mutex
	deleted []string
}

func automaticS3TestTransport(t *testing.T, objects []automaticS3TestObject, statusByMethod map[string]int) *automaticS3TestRoundTripper {
	t.Helper()

	return &automaticS3TestRoundTripper{
		t:       t,
		objects: objects,
		status:  statusByMethod,
	}
}

func (rt *automaticS3TestRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	if status := rt.status[r.Method]; status != 0 {
		return automaticS3TestResponse(r, status, "forced storage error"), nil
	}

	switch r.Method {
	case http.MethodGet:
		if strings.Contains(r.URL.RawQuery, "location") {
			return automaticS3TestResponse(r, http.StatusOK, `<LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></LocationConstraint>`), nil
		}
		var body strings.Builder
		body.WriteString(`<?xml version="1.0" encoding="UTF-8"?><ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>xlyra</Name><Prefix>prod/</Prefix><KeyCount>`)
		fmt.Fprint(&body, len(rt.objects))
		body.WriteString(`</KeyCount><MaxKeys>1000</MaxKeys><IsTruncated>false</IsTruncated>`)
		for _, object := range rt.objects {
			fmt.Fprintf(&body, `<Contents><Key>%s</Key><LastModified>%s</LastModified><ETag>"etag"</ETag><Size>%d</Size><StorageClass>STANDARD</StorageClass></Contents>`, object.Key, object.LastModified, object.Size)
		}
		body.WriteString(`</ListBucketResult>`)
		return automaticS3TestResponse(r, http.StatusOK, body.String()), nil
	case http.MethodDelete:
		key, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/xlyra/"))
		if err != nil {
			return automaticS3TestResponse(r, http.StatusBadRequest, "bad key"), nil
		}
		rt.mu.Lock()
		rt.deleted = append(rt.deleted, key)
		rt.mu.Unlock()
		return automaticS3TestResponse(r, http.StatusNoContent, ""), nil
	case http.MethodPut:
		_, _ = io.Copy(io.Discard, r.Body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"ETag": []string{`"test-etag"`}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    r,
		}, nil
	default:
		return automaticS3TestResponse(r, http.StatusMethodNotAllowed, "unexpected method"), nil
	}
}

func (rt *automaticS3TestRoundTripper) deletedKeys() []string {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return append([]string(nil), rt.deleted...)
}

func automaticS3TestResponse(r *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     http.Header{"Content-Type": []string{"application/xml"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    r,
	}
}

func automaticS3TestClient(t *testing.T, transport http.RoundTripper) *minio.Client {
	t.Helper()

	client, err := minio.New("s3.example.com", &minio.Options{
		Creds:        credentials.NewStaticV4("access", "secret", ""),
		Secure:       false,
		BucketLookup: minio.BucketLookupPath,
		Transport:    transport,
	})
	if err != nil {
		t.Fatalf("minio client: %v", err)
	}
	return client
}

func automaticS3TestConfig() config.AutomaticBackupConfig {
	return config.NormalizeAutomaticBackupConfig(config.AutomaticBackupConfig{
		Cron:           "0 3 * * *",
		RetentionCount: 7,
		Storage: config.AutomaticBackupStorageConfig{
			Endpoint:       "s3.example.com",
			Bucket:         "xlyra",
			Prefix:         "prod",
			AccessKey:      "access",
			ForcePathStyle: true,
		},
	})
}
