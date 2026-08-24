package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func writePlaygroundTestFile(t *testing.T, root string, relPath string, content string) {
	t.Helper()
	absPath := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(absPath), 0o750); err != nil {
		t.Fatalf("create test file dir: %v", err)
	}
	if err := os.WriteFile(absPath, []byte(content), 0o640); err != nil {
		t.Fatalf("write test file: %v", err)
	}
}

func readZipEntry(t *testing.T, archivePath string, name string) string {
	t.Helper()
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer zr.Close()
	file := archiveFile(zr.File, name)
	if file == nil {
		names := make([]string, 0, len(zr.File))
		for _, entry := range zr.File {
			names = append(names, entry.Name)
		}
		t.Fatalf("archive entry %s not found in %#v", name, names)
	}
	reader, err := file.Open()
	if err != nil {
		t.Fatalf("open archive entry %s: %v", name, err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read archive entry %s: %v", name, err)
	}
	return string(data)
}

func zipEntryMethod(t *testing.T, archivePath string, name string) uint16 {
	t.Helper()
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer zr.Close()
	file := archiveFile(zr.File, name)
	if file == nil {
		t.Fatalf("archive entry %s not found", name)
	}
	return file.Method
}

func TestExportPlaygroundFileEntriesTruncatesRolloutAtCursor(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	line1 := `{"ordinal":1,"type":"session_meta"}` + "\n"
	line2 := `{"ordinal":2,"type":"message_added"}` + "\n"
	relPath := "sessions/2026/08/24/rollout-test.jsonl"
	writePlaygroundTestFile(t, root, relPath, line1+line2)
	assetRel := "assets/conv-1/generated-images/asset-1.png"
	writePlaygroundTestFile(t, root, assetRel, "png-bytes")

	conversationID := uuid.New()
	assetID := uuid.New()
	conversations := []store.PlaygroundConversation{{
		ID:             conversationID,
		RolloutPath:    relPath,
		LastByteOffset: int64(len(line1)),
	}}
	assets := []store.PlaygroundAsset{{ID: assetID, ConversationID: conversationID, Path: assetRel}}

	archivePath := filepath.Join(t.TempDir(), "files.zip")
	out, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	zw := zip.NewWriter(out)
	files, total, err := exportPlaygroundFileEntries(context.Background(), nil, zw, root, conversations, assets)
	if err != nil {
		t.Fatalf("export playground files: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close archive file: %v", err)
	}

	if files != 2 {
		t.Fatalf("expected 2 exported files, got %d", files)
	}
	if total != int64(len(line1)+len("png-bytes")) {
		t.Fatalf("unexpected exported bytes: %d", total)
	}
	if got := readZipEntry(t, archivePath, "playground/"+relPath); got != line1 {
		t.Fatalf("rollout not truncated at cursor:\n got %q\nwant %q", got, line1)
	}
	if got := readZipEntry(t, archivePath, "playground/"+assetRel); got != "png-bytes" {
		t.Fatalf("asset payload mismatch: %q", got)
	}
	if method := zipEntryMethod(t, archivePath, "playground/"+relPath); method != zip.Deflate {
		t.Fatalf("expected rollout to use deflate, got method %d", method)
	}
	if method := zipEntryMethod(t, archivePath, "playground/"+assetRel); method != zip.Store {
		t.Fatalf("expected image asset to be stored uncompressed, got method %d", method)
	}
}

func TestExportPlaygroundFileEntriesRejectsInconsistentSources(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	relPath := "sessions/2026/08/24/rollout-test.jsonl"

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	conversations := []store.PlaygroundConversation{{ID: uuid.New(), RolloutPath: relPath, LastByteOffset: 10}}
	if _, _, err := exportPlaygroundFileEntries(context.Background(), nil, zw, root, conversations, nil); err == nil || !strings.Contains(err.Error(), "open") {
		t.Fatalf("expected missing rollout error, got %v", err)
	}

	writePlaygroundTestFile(t, root, relPath, "short\n")
	buf.Reset()
	zw = zip.NewWriter(&buf)
	if _, _, err := exportPlaygroundFileEntries(context.Background(), nil, zw, root, conversations, nil); err == nil || !strings.Contains(err.Error(), "shorter than recorded cursor") {
		t.Fatalf("expected short rollout error, got %v", err)
	}

	buf.Reset()
	zw = zip.NewWriter(&buf)
	conversations[0].RolloutPath = "../outside.jsonl"
	if _, _, err := exportPlaygroundFileEntries(context.Background(), nil, zw, root, conversations, nil); err == nil || !strings.Contains(err.Error(), "invalid relative path") {
		t.Fatalf("expected traversal error, got %v", err)
	}
}

func TestRestorePlaygroundFilesReplacesDirectories(t *testing.T) {
	t.Parallel()

	archivePath := filepath.Join(t.TempDir(), "files.zip")
	out, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	zw := zip.NewWriter(out)
	for name, content := range map[string]string{
		"playground/sessions/2026/08/24/rollout-new.jsonl": "new-rollout\n",
		"playground/assets/conv-1/generated-images/a.png":  "new-asset",
		"manifest.json": "{}",
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create archive entry: %v", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("write archive entry: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close archive file: %v", err)
	}

	root := t.TempDir()
	writePlaygroundTestFile(t, root, "sessions/2026/01/01/rollout-old.jsonl", "old-rollout\n")
	writePlaygroundTestFile(t, root, "assets/conv-old/generated-images/old.png", "old-asset")
	writePlaygroundTestFile(t, root, "thread-writer-locks/conv-old.lock", "")

	var progressed int64
	files, total, err := restorePlaygroundFiles(archivePath, root, func(bytes int64) { progressed = bytes })
	if err != nil {
		t.Fatalf("restore playground files: %v", err)
	}
	if files != 2 {
		t.Fatalf("expected 2 restored files, got %d", files)
	}
	if total != int64(len("new-rollout\n")+len("new-asset")) || progressed != total {
		t.Fatalf("unexpected restored bytes %d (progress %d)", total, progressed)
	}

	restored, err := os.ReadFile(filepath.Join(root, "sessions", "2026", "08", "24", "rollout-new.jsonl"))
	if err != nil || string(restored) != "new-rollout\n" {
		t.Fatalf("restored rollout mismatch: %q, %v", restored, err)
	}
	if _, err := os.Stat(filepath.Join(root, "sessions", "2026", "01", "01", "rollout-old.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("expected stale rollout to be removed, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "assets", "conv-old")); !os.IsNotExist(err) {
		t.Fatalf("expected stale assets to be removed, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "thread-writer-locks", "conv-old.lock")); err != nil {
		t.Fatalf("expected transient lock directory to survive: %v", err)
	}
	info, err := os.Stat(filepath.Join(root, "assets", "conv-1", "generated-images", "a.png"))
	if err != nil {
		t.Fatalf("restored asset missing: %v", err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("expected 0640 permissions, got %v", info.Mode().Perm())
	}
}

func TestRestorePlaygroundFilesClearsDirectoriesWithoutPayload(t *testing.T) {
	t.Parallel()

	archivePath := filepath.Join(t.TempDir(), "empty.zip")
	out, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	zw := zip.NewWriter(out)
	w, err := zw.Create("manifest.json")
	if err != nil {
		t.Fatalf("create manifest entry: %v", err)
	}
	if _, err := w.Write([]byte("{}")); err != nil {
		t.Fatalf("write manifest entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close archive file: %v", err)
	}

	root := t.TempDir()
	writePlaygroundTestFile(t, root, "sessions/2026/01/01/rollout-old.jsonl", "old\n")
	writePlaygroundTestFile(t, root, "assets/conv-old/generated-images/old.png", "old")

	files, total, err := restorePlaygroundFiles(archivePath, root, nil)
	if err != nil {
		t.Fatalf("restore playground files: %v", err)
	}
	if files != 0 || total != 0 {
		t.Fatalf("expected no restored files, got %d files %d bytes", files, total)
	}
	for _, dir := range playgroundDirs {
		if entries, err := os.ReadDir(filepath.Join(root, dir)); err == nil && len(entries) > 0 {
			t.Fatalf("expected %s to be empty after restore, found %d entries", dir, len(entries))
		}
	}
}

func TestRestorePlaygroundFilesRejectsTraversal(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"parent traversal": "playground/../evil.txt",
		"nested traversal": "playground/sessions/../../evil.txt",
		"unexpected dir":   "playground/other/file.txt",
		"backslash":        `playground/sessions\evil.txt`,
		"absolute":         "playground//etc/passwd",
	}
	for name, entryName := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			writePlaygroundTestFile(t, root, "sessions/existing.jsonl", "keep\n")

			archivePath := filepath.Join(t.TempDir(), "evil.zip")
			out, err := os.Create(archivePath)
			if err != nil {
				t.Fatalf("create archive: %v", err)
			}
			zw := zip.NewWriter(out)
			w, err := zw.Create(entryName)
			if err != nil {
				t.Fatalf("create archive entry: %v", err)
			}
			if _, err := w.Write([]byte("evil")); err != nil {
				t.Fatalf("write archive entry: %v", err)
			}
			if err := zw.Close(); err != nil {
				t.Fatalf("close archive: %v", err)
			}
			if err := out.Close(); err != nil {
				t.Fatalf("close archive file: %v", err)
			}

			if _, _, err := restorePlaygroundFiles(archivePath, root, nil); err == nil || !strings.Contains(err.Error(), "playground path") {
				t.Fatalf("expected invalid path error, got %v", err)
			}
			content, err := os.ReadFile(filepath.Join(root, "sessions", "existing.jsonl"))
			if err != nil || string(content) != "keep\n" {
				t.Fatalf("existing session file modified: %q, %v", content, err)
			}
		})
	}
}

func TestReownPlaygroundConversations(t *testing.T) {
	t.Parallel()

	adminID := uuid.New()
	rows := []map[string]any{
		{"id": uuid.New().String(), "admin_id": uuid.New().String()},
		{"id": uuid.New().String(), "admin_id": nil},
	}
	reownPlaygroundConversations(rows, adminID)
	for i, row := range rows {
		if row["admin_id"] != adminID.String() {
			t.Fatalf("row %d admin_id = %#v, want %s", i, row["admin_id"], adminID)
		}
	}
}
