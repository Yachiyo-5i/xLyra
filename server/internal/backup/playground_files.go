package backup

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

const (
	playgroundArchivePrefix = "playground/"
	maxPlaygroundFileBytes  = 1 << 30
	maxPlaygroundTotalBytes = 4 << 30
)

var playgroundDirs = []string{"sessions", "assets"}

func listPlaygroundFileRefs(ctx context.Context, db *gorm.DB) ([]store.PlaygroundConversation, []store.PlaygroundAsset, error) {
	var conversations []store.PlaygroundConversation
	if err := db.WithContext(ctx).Find(&conversations).Error; err != nil {
		return nil, nil, fmt.Errorf("list playground conversations for file export: %w", err)
	}
	var assets []store.PlaygroundAsset
	if err := db.WithContext(ctx).Find(&assets).Error; err != nil {
		return nil, nil, fmt.Errorf("list playground assets for file export: %w", err)
	}
	return conversations, assets, nil
}

func exportPlaygroundFileEntries(ctx context.Context, db *gorm.DB, zw *zip.Writer, root string, conversations []store.PlaygroundConversation, assets []store.PlaygroundAsset) (int, int64, error) {
	files := 0
	var total int64
	for _, conversation := range conversations {
		relPath, err := cleanArchivedRelativePath(conversation.RolloutPath)
		if err != nil {
			return files, total, fmt.Errorf("export rollout for conversation %s: %w", conversation.ID, err)
		}
		if conversation.LastByteOffset <= 0 {
			continue
		}
		written, copyErr := zipPlaygroundFile(zw, relPath, filepath.Join(root, filepath.FromSlash(relPath)), conversation.LastByteOffset)
		short := copyErr == nil && written < conversation.LastByteOffset
		if copyErr != nil || short {
			if playgroundRowGone(ctx, db, &store.PlaygroundConversation{}, &store.PlaygroundConversation{ID: conversation.ID}) {
				continue
			}
			if copyErr != nil {
				return files, total, fmt.Errorf("export rollout for conversation %s: %w", conversation.ID, copyErr)
			}
			return files, total, fmt.Errorf("export rollout for conversation %s: file shorter than recorded cursor (%d < %d)", conversation.ID, written, conversation.LastByteOffset)
		}
		files++
		total += written
	}
	for _, asset := range assets {
		relPath, err := cleanArchivedRelativePath(asset.Path)
		if err != nil {
			return files, total, fmt.Errorf("export playground asset %s: %w", asset.ID, err)
		}
		written, err := zipPlaygroundFile(zw, relPath, filepath.Join(root, filepath.FromSlash(relPath)), -1)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && playgroundRowGone(ctx, db, &store.PlaygroundAsset{}, &store.PlaygroundAsset{ID: asset.ID}) {
				continue
			}
			return files, total, fmt.Errorf("export playground asset %s: %w", asset.ID, err)
		}
		files++
		total += written
	}
	return files, total, nil
}

func playgroundRowGone(ctx context.Context, db *gorm.DB, model any, condition any) bool {
	if db == nil {
		return false
	}
	var count int64
	if err := db.WithContext(ctx).Model(model).Where(condition).Count(&count).Error; err != nil {
		return false
	}
	return count == 0
}

func cleanArchivedRelativePath(relPath string) (string, error) {
	relPath = strings.TrimSpace(filepath.ToSlash(relPath))
	clean := path.Clean(relPath)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) || strings.ContainsRune(relPath, '\\') {
		return "", fmt.Errorf("invalid relative path %q", relPath)
	}
	return clean, nil
}

func zipPlaygroundFile(zw *zip.Writer, relPath string, absPath string, limit int64) (int64, error) {
	file, err := os.Open(absPath)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", relPath, err)
	}
	defer file.Close()

	w, err := createArchiveEntry(zw, playgroundArchivePrefix+relPath, zipMethodForFile(relPath))
	if err != nil {
		return 0, err
	}
	source := io.Reader(file)
	if limit >= 0 {
		source = io.LimitReader(file, limit)
	}
	written, err := io.Copy(w, source)
	if err != nil {
		return written, fmt.Errorf("copy %s: %w", relPath, err)
	}
	return written, nil
}

func zipMethodForFile(relPath string) uint16 {
	switch strings.ToLower(path.Ext(relPath)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".mp4", ".webm", ".zip", ".gz":
		return zip.Store
	default:
		return zip.Deflate
	}
}

func restorePlaygroundFiles(archivePath string, root string, progress func(int64)) (int, int64, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return 0, 0, fmt.Errorf("open backup archive: %w", err)
	}
	defer zr.Close()

	if err := os.MkdirAll(root, 0o750); err != nil {
		return 0, 0, fmt.Errorf("create playground root: %w", err)
	}
	staging, err := os.MkdirTemp(root, ".restore-staging-")
	if err != nil {
		return 0, 0, fmt.Errorf("create playground restore staging: %w", err)
	}
	defer os.RemoveAll(staging)

	files := 0
	var total int64
	for _, entry := range zr.File {
		if !strings.HasPrefix(entry.Name, playgroundArchivePrefix) {
			continue
		}
		written, isFile, err := extractPlaygroundEntry(entry, staging)
		if err != nil {
			return files, total, err
		}
		if !isFile {
			continue
		}
		files++
		total += written
		if total > maxPlaygroundTotalBytes {
			return files, total, fmt.Errorf("playground payload exceeds maximum total size")
		}
		if progress != nil {
			progress(total)
		}
	}

	if err := swapPlaygroundDirs(root, staging); err != nil {
		return files, total, err
	}
	return files, total, nil
}

func extractPlaygroundEntry(entry *zip.File, staging string) (int64, bool, error) {
	relPath, err := cleanArchivePlaygroundPath(entry.Name)
	if err != nil {
		return 0, false, err
	}
	if relPath == "" {
		return 0, false, nil
	}
	if entry.UncompressedSize64 > maxPlaygroundFileBytes {
		return 0, false, fmt.Errorf("playground file %s exceeds maximum size", relPath)
	}
	reader, err := entry.Open()
	if err != nil {
		return 0, false, fmt.Errorf("open archived playground file %s: %w", relPath, err)
	}
	defer reader.Close()

	target := filepath.Join(staging, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return 0, false, fmt.Errorf("create playground directory for %s: %w", relPath, err)
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return 0, false, fmt.Errorf("create playground file %s: %w", relPath, err)
	}
	written, copyErr := io.Copy(out, io.LimitReader(reader, maxPlaygroundFileBytes+1))
	closeErr := out.Close()
	if copyErr != nil {
		return 0, false, fmt.Errorf("extract playground file %s: %w", relPath, copyErr)
	}
	if closeErr != nil {
		return 0, false, fmt.Errorf("close playground file %s: %w", relPath, closeErr)
	}
	if written > maxPlaygroundFileBytes {
		return 0, false, fmt.Errorf("playground file %s exceeds maximum size", relPath)
	}
	return written, true, nil
}

func cleanArchivePlaygroundPath(name string) (string, error) {
	relPath := strings.TrimPrefix(name, playgroundArchivePrefix)
	clean := path.Clean(relPath)
	if clean == "." {
		return "", nil
	}
	if clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) || strings.ContainsRune(relPath, '\\') {
		return "", fmt.Errorf("backup archive contains invalid playground path %q", name)
	}
	top, _, _ := strings.Cut(clean, "/")
	if !slices.Contains(playgroundDirs, top) {
		return "", fmt.Errorf("backup archive contains unexpected playground path %q", name)
	}
	if strings.HasSuffix(name, "/") {
		return "", nil
	}
	return clean, nil
}

func swapPlaygroundDirs(root string, staging string) error {
	trashed := make([]string, 0, len(playgroundDirs))
	rollback := func() {
		for _, dir := range trashed {
			_ = os.Rename(filepath.Join(root, ".trash-"+dir), filepath.Join(root, dir))
		}
	}
	for _, dir := range playgroundDirs {
		current := filepath.Join(root, dir)
		trash := filepath.Join(root, ".trash-"+dir)
		_ = os.RemoveAll(trash)
		if !dirExists(current) {
			continue
		}
		if err := os.Rename(current, trash); err != nil {
			rollback()
			return fmt.Errorf("set aside existing playground %s: %w", dir, err)
		}
		trashed = append(trashed, dir)
	}
	installed := make([]string, 0, len(playgroundDirs))
	for _, dir := range playgroundDirs {
		staged := filepath.Join(staging, dir)
		if !dirExists(staged) {
			continue
		}
		if err := os.Rename(staged, filepath.Join(root, dir)); err != nil {
			for _, done := range installed {
				_ = os.RemoveAll(filepath.Join(root, done))
			}
			rollback()
			return fmt.Errorf("install restored playground %s: %w", dir, err)
		}
		installed = append(installed, dir)
	}
	for _, dir := range trashed {
		if err := os.RemoveAll(filepath.Join(root, ".trash-"+dir)); err != nil {
			return fmt.Errorf("remove previous playground %s: %w", dir, err)
		}
	}
	return nil
}

func dirExists(path string) bool {
	stat, err := os.Stat(path)
	return err == nil && stat.IsDir()
}
