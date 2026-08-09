package backup

import (
	"context"
	"encoding/xml"
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

	files, err = service.listFilesWithClient(context.Background(), cfg, client, 0)
	if err != nil {
		t.Fatalf("listFilesWithClient without limit: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected every backup file without a limit, got %#v", files)
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

func TestAutomaticPruneBackupsRemovesAllVersionsOfExpiredBackups(t *testing.T) {
	t.Parallel()

	transport := automaticS3TestTransport(t, []automaticS3TestObject{
		{Key: "prod/xlyra-backup-20260623-010000.zip.xlyra", Size: 23, LastModified: "2026-06-23T01:00:00.000Z", VersionID: "new-current", IsLatest: true},
		{Key: "prod/xlyra-backup-20260623-010000.zip.xlyra", Size: 22, LastModified: "2026-06-22T01:00:00.000Z", VersionID: "new-previous"},
		{Key: "prod/xlyra-backup-20260621-010000.zip.xlyra", Size: 21, LastModified: "2026-06-21T01:00:00.000Z", VersionID: "old-current", IsLatest: true},
		{Key: "prod/xlyra-backup-20260620-010000.zip.xlyra", LastModified: "2026-06-20T01:00:00.000Z", VersionID: "old-marker", IsLatest: true, DeleteMarker: true},
		{Key: "prod/xlyra-backup-20260620-010000.zip.xlyra", Size: 20, LastModified: "2026-06-19T01:00:00.000Z", VersionID: "old-previous"},
	}, nil).withVersioning("Enabled")
	service := NewAutomaticService(Service{}, "master-key")
	cfg := automaticS3TestConfig()
	cfg.RetentionCount = 1
	client := automaticS3TestClient(t, transport)

	deleted, err := service.pruneBackups(context.Background(), cfg, client)
	if err != nil {
		t.Fatalf("pruneBackups: %v", err)
	}
	if got := strings.Join(deleted, ","); got != "prod/xlyra-backup-20260621-010000.zip.xlyra,prod/xlyra-backup-20260620-010000.zip.xlyra" {
		t.Fatalf("deleted = %q", got)
	}
	if got := strings.Join(transport.deletedVersionIDs(), ","); got != "new-previous,old-current,old-marker,old-previous" {
		t.Fatalf("deleted version IDs = %q", got)
	}
	files, err := service.listFilesWithClient(context.Background(), cfg, client, 0)
	if err != nil {
		t.Fatalf("listFilesWithClient: %v", err)
	}
	if len(files) != 1 || files[0].Key != "prod/xlyra-backup-20260623-010000.zip.xlyra" {
		t.Fatalf("remaining files = %#v", files)
	}
}

func TestAutomaticDeleteBackupVersionsBatchesMoreThanOneThousandObjects(t *testing.T) {
	t.Parallel()

	objects := make([]automaticS3TestObject, 0, 1001)
	targets := make([]automaticBackupVersion, 0, 1001)
	for i := 0; i < 1001; i++ {
		key := fmt.Sprintf("prod/xlyra-backup-%04d.zip.xlyra", i)
		versionID := fmt.Sprintf("version-%04d", i)
		objects = append(objects, automaticS3TestObject{
			Key:          key,
			LastModified: "2026-06-23T01:00:00.000Z",
			VersionID:    versionID,
			IsLatest:     true,
		})
		targets = append(targets, automaticBackupVersion{Key: key, VersionID: versionID})
	}
	transport := automaticS3TestTransport(t, objects, nil).withVersioning("Enabled")
	service := NewAutomaticService(Service{}, "master-key")
	cfg := automaticS3TestConfig()
	client := automaticS3TestClient(t, transport)

	if err := service.deleteBackupVersions(context.Background(), cfg, client, targets); err != nil {
		t.Fatalf("deleteBackupVersions: %v", err)
	}
	if got := len(transport.deletedVersionIDs()); got != len(targets) {
		t.Fatalf("deleted versions = %d, want %d", got, len(targets))
	}
	if got := transport.deleteBatchCount(); got != 2 {
		t.Fatalf("delete batches = %d, want 2", got)
	}
}

func TestAutomaticPruneBackupsFailsWhenDeletedObjectRemains(t *testing.T) {
	t.Parallel()

	transport := automaticS3TestTransport(t, []automaticS3TestObject{
		{Key: "prod/xlyra-backup-20260623-010000.zip.xlyra", Size: 23, LastModified: "2026-06-23T01:00:00.000Z"},
		{Key: "prod/xlyra-backup-20260622-010000.zip.xlyra", Size: 22, LastModified: "2026-06-22T01:00:00.000Z"},
	}, nil).withIgnoredDeletes()
	service := NewAutomaticService(Service{}, "master-key")
	cfg := automaticS3TestConfig()
	cfg.RetentionCount = 1
	client := automaticS3TestClient(t, transport)

	_, err := service.pruneBackups(context.Background(), cfg, client)
	if err == nil || !strings.Contains(err.Error(), "verify deleted backup prod/xlyra-backup-20260622-010000.zip.xlyra") {
		t.Fatalf("pruneBackups error = %v", err)
	}
}

func TestAutomaticListAndPrunePropagateStorageErrors(t *testing.T) {
	t.Parallel()

	service := NewAutomaticService(Service{}, "master-key")
	cfg := automaticS3TestConfig()
	client := automaticS3TestClient(t, automaticS3TestTransport(t, nil, map[string]int{http.MethodGet: http.StatusInternalServerError}))
	if _, err := service.listFilesWithClient(context.Background(), cfg, client, 0); err == nil || !strings.Contains(err.Error(), "list backup files from S3") {
		t.Fatalf("listFilesWithClient error = %v, want storage list context", err)
	}

	transport := automaticS3TestTransport(t, []automaticS3TestObject{
		{Key: "prod/xlyra-backup-20260623-010000.zip.xlyra", Size: 23, LastModified: "2026-06-23T01:00:00.000Z"},
		{Key: "prod/xlyra-backup-20260622-010000.zip.xlyra", Size: 22, LastModified: "2026-06-22T01:00:00.000Z"},
	}, map[string]int{http.MethodDelete: http.StatusInternalServerError})
	cfg.RetentionCount = 1
	client = automaticS3TestClient(t, transport)
	deleted, err := service.pruneBackups(context.Background(), cfg, client)
	if err == nil || !strings.Contains(err.Error(), "delete backup prod/xlyra-backup-20260622-010000.zip.xlyra") {
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
	VersionID    string
	IsLatest     bool
	DeleteMarker bool
}

type automaticS3DeleteRequest struct {
	Objects []automaticS3DeleteObject `xml:"Object"`
}

type automaticS3DeleteObject struct {
	Key       string `xml:"Key"`
	VersionID string `xml:"VersionId"`
}

type automaticS3TestRoundTripper struct {
	t             *testing.T
	objects       []automaticS3TestObject
	status        map[string]int
	versioning    string
	ignoreDelete  bool
	mu            sync.Mutex
	deleted       []string
	versions      []string
	deleteBatches int
}

func (rt *automaticS3TestRoundTripper) withVersioning(status string) *automaticS3TestRoundTripper {
	rt.versioning = status
	return rt
}

func (rt *automaticS3TestRoundTripper) withIgnoredDeletes() *automaticS3TestRoundTripper {
	rt.ignoreDelete = true
	return rt
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
		if r.URL.Query().Has("versioning") {
			return automaticS3TestResponse(r, http.StatusOK, fmt.Sprintf(`<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>%s</Status></VersioningConfiguration>`, rt.versioning)), nil
		}
		rt.mu.Lock()
		objects := append([]automaticS3TestObject(nil), rt.objects...)
		rt.mu.Unlock()
		if r.URL.Query().Has("versions") {
			var body strings.Builder
			body.WriteString(`<?xml version="1.0" encoding="UTF-8"?><ListVersionsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>xlyra</Name><Prefix>prod/</Prefix><IsTruncated>false</IsTruncated>`)
			for _, object := range objects {
				if object.DeleteMarker {
					fmt.Fprintf(&body, `<DeleteMarker><Key>%s</Key><VersionId>%s</VersionId><IsLatest>%t</IsLatest><LastModified>%s</LastModified><Owner><ID>test</ID><DisplayName>test</DisplayName></Owner></DeleteMarker>`, object.Key, object.VersionID, object.IsLatest, object.LastModified)
					continue
				}
				fmt.Fprintf(&body, `<Version><Key>%s</Key><VersionId>%s</VersionId><IsLatest>%t</IsLatest><LastModified>%s</LastModified><ETag>etag</ETag><Size>%d</Size><StorageClass>STANDARD</StorageClass><Owner><ID>test</ID><DisplayName>test</DisplayName></Owner></Version>`, object.Key, object.VersionID, object.IsLatest, object.LastModified, object.Size)
			}
			body.WriteString(`</ListVersionsResult>`)
			return automaticS3TestResponse(r, http.StatusOK, body.String()), nil
		}
		var body strings.Builder
		body.WriteString(`<?xml version="1.0" encoding="UTF-8"?><ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>xlyra</Name><Prefix>prod/</Prefix><KeyCount>`)
		fmt.Fprint(&body, len(objects))
		body.WriteString(`</KeyCount><MaxKeys>1000</MaxKeys><IsTruncated>false</IsTruncated>`)
		for _, object := range objects {
			if object.DeleteMarker || !object.IsLatest && object.VersionID != "" {
				continue
			}
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
		versionID := r.URL.Query().Get("versionId")
		rt.versions = append(rt.versions, versionID)
		if !rt.ignoreDelete {
			remaining := rt.objects[:0]
			for _, object := range rt.objects {
				if object.Key != key || versionID != "" && object.VersionID != versionID {
					remaining = append(remaining, object)
				}
			}
			rt.objects = remaining
		}
		rt.mu.Unlock()
		return automaticS3TestResponse(r, http.StatusNoContent, ""), nil
	case http.MethodPost:
		if !r.URL.Query().Has("delete") {
			return automaticS3TestResponse(r, http.StatusMethodNotAllowed, "unexpected request"), nil
		}
		var request automaticS3DeleteRequest
		if err := xml.NewDecoder(r.Body).Decode(&request); err != nil {
			return automaticS3TestResponse(r, http.StatusBadRequest, "invalid delete request"), nil
		}
		rt.mu.Lock()
		rt.deleteBatches++
		for _, object := range request.Objects {
			rt.deleted = append(rt.deleted, object.Key)
			rt.versions = append(rt.versions, object.VersionID)
			if rt.ignoreDelete {
				continue
			}
			remaining := rt.objects[:0]
			for _, current := range rt.objects {
				if current.Key != object.Key || object.VersionID != "" && current.VersionID != object.VersionID {
					remaining = append(remaining, current)
				}
			}
			rt.objects = remaining
		}
		rt.mu.Unlock()
		return automaticS3TestResponse(r, http.StatusOK, `<DeleteResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></DeleteResult>`), nil
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

func (rt *automaticS3TestRoundTripper) deletedVersionIDs() []string {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return append([]string(nil), rt.versions...)
}

func (rt *automaticS3TestRoundTripper) deleteBatchCount() int {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.deleteBatches
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
