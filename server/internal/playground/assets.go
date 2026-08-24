package playground

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

const maxAssetBytes = 25 * 1024 * 1024

type AssetStore struct {
	root   string
	repo   store.PlaygroundRepository
	client *http.Client
}

func NewAssetStore(root string, repo store.PlaygroundRepository) *AssetStore {
	assetStore := &AssetStore{root: root, repo: repo}
	transport := &http.Transport{DialContext: dialPublicAddress}
	assetStore.client = &http.Client{
		Timeout:   45 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many image redirects")
			}
			return validateRemoteURL(req.Context(), req.URL)
		},
	}
	return assetStore
}

func dialPublicAddress(ctx context.Context, network string, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, address := range addresses {
		ip := address.IP
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			continue
		}
		return (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
	}
	return nil, fmt.Errorf("image URL has no public address")
}

func (s *AssetStore) SaveDataURL(ctx context.Context, conversationID uuid.UUID, runID uuid.UUID, purpose string, name string, value string) (store.PlaygroundAsset, error) {
	comma := strings.IndexByte(value, ',')
	if comma < 0 || !strings.HasPrefix(value, "data:") {
		return store.PlaygroundAsset{}, fmt.Errorf("invalid data URL")
	}
	meta := value[5:comma]
	if !strings.Contains(meta, ";base64") {
		return store.PlaygroundAsset{}, fmt.Errorf("asset data URL must be base64 encoded")
	}
	mimeType := strings.TrimSpace(strings.Split(meta, ";")[0])
	data, err := base64.StdEncoding.DecodeString(value[comma+1:])
	if err != nil {
		return store.PlaygroundAsset{}, fmt.Errorf("decode asset data URL: %w", err)
	}
	return s.SaveBytes(ctx, conversationID, runID, purpose, name, mimeType, data)
}

func (s *AssetStore) SaveBytes(ctx context.Context, conversationID uuid.UUID, runID uuid.UUID, purpose string, name string, mimeType string, data []byte) (store.PlaygroundAsset, error) {
	if len(data) == 0 || len(data) > maxAssetBytes {
		return store.PlaygroundAsset{}, fmt.Errorf("asset size is invalid")
	}
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = http.DetectContentType(data)
	}
	id := uuid.New()
	ext := extensionForMIME(mimeType)
	relPath := filepath.ToSlash(filepath.Join("assets", conversationID.String(), purpose, id.String()+ext))
	absPath := filepath.Join(s.root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(absPath), 0o750); err != nil {
		return store.PlaygroundAsset{}, fmt.Errorf("create asset directory: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(absPath), ".asset-*")
	if err != nil {
		return store.PlaygroundAsset{}, fmt.Errorf("create asset file: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o640); err != nil {
		_ = temp.Close()
		return store.PlaygroundAsset{}, err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return store.PlaygroundAsset{}, fmt.Errorf("write asset: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return store.PlaygroundAsset{}, fmt.Errorf("sync asset: %w", err)
	}
	if err := temp.Close(); err != nil {
		return store.PlaygroundAsset{}, err
	}
	if err := os.Rename(tempPath, absPath); err != nil {
		return store.PlaygroundAsset{}, fmt.Errorf("publish asset: %w", err)
	}
	digest := sha256.Sum256(data)
	item := store.PlaygroundAsset{
		ID: id, ConversationID: conversationID, Purpose: purpose, Path: relPath,
		OriginalName: name, MIMEType: mimeType, Size: int64(len(data)), SHA256: fmt.Sprintf("%x", digest),
	}
	if runID != uuid.Nil {
		item.RunID = uuid.NullUUID{UUID: runID, Valid: true}
	}
	if err := s.repo.CreateAsset(ctx, &item); err != nil {
		_ = os.Remove(absPath)
		return store.PlaygroundAsset{}, err
	}
	return item, nil
}

func (s *AssetStore) SaveRemote(ctx context.Context, conversationID uuid.UUID, runID uuid.UUID, purpose string, value string) (store.PlaygroundAsset, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return store.PlaygroundAsset{}, fmt.Errorf("parse image URL: %w", err)
	}
	if err := validateRemoteURL(ctx, parsed); err != nil {
		return store.PlaygroundAsset{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return store.PlaygroundAsset{}, err
	}
	response, err := s.client.Do(request)
	if err != nil {
		return store.PlaygroundAsset{}, fmt.Errorf("download image: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return store.PlaygroundAsset{}, fmt.Errorf("download image returned status %d", response.StatusCode)
	}
	limited := io.LimitReader(response.Body, maxAssetBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return store.PlaygroundAsset{}, fmt.Errorf("read downloaded image: %w", err)
	}
	if len(data) > maxAssetBytes {
		return store.PlaygroundAsset{}, fmt.Errorf("downloaded image is too large")
	}
	mimeType := strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0])
	if mimeType == "" {
		mimeType = http.DetectContentType(data)
	}
	if !strings.HasPrefix(mimeType, "image/") {
		return store.PlaygroundAsset{}, fmt.Errorf("downloaded resource is not an image")
	}
	name := filepath.Base(parsed.Path)
	return s.SaveBytes(ctx, conversationID, runID, purpose, name, mimeType, data)
}

func validateRemoteURL(ctx context.Context, parsed *url.URL) error {
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("image URL scheme is not allowed")
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("image URL host is required")
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve image URL host: %w", err)
	}
	for _, address := range addresses {
		ip := address.IP
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return fmt.Errorf("image URL resolves to a private address")
		}
	}
	return nil
}

func extensionForMIME(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/avif":
		return ".avif"
	case "application/pdf":
		return ".pdf"
	case "text/plain":
		return ".txt"
	case "application/json":
		return ".json"
	default:
		return ".png"
	}
}

func (s *AssetStore) Read(ctx context.Context, id uuid.UUID) (store.PlaygroundAsset, []byte, error) {
	item, err := s.repo.GetAsset(ctx, id)
	if err != nil {
		return store.PlaygroundAsset{}, nil, err
	}
	path := filepath.Join(s.root, filepath.FromSlash(item.Path))
	data, err := os.ReadFile(path)
	if err != nil {
		return store.PlaygroundAsset{}, nil, err
	}
	return item, data, nil
}

func (s *AssetStore) Delete(ctx context.Context, asset store.PlaygroundAsset) error {
	if err := s.repo.DeleteAsset(ctx, asset.ID); err != nil {
		return err
	}
	path := filepath.Join(s.root, filepath.FromSlash(asset.Path))
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *AssetStore) DataURL(ctx context.Context, id uuid.UUID) (string, error) {
	item, data, err := s.Read(ctx, id)
	if err != nil {
		return "", err
	}
	var builder bytes.Buffer
	builder.WriteString("data:")
	builder.WriteString(item.MIMEType)
	builder.WriteString(";base64,")
	encoder := base64.NewEncoder(base64.StdEncoding, &builder)
	if _, err := encoder.Write(data); err != nil {
		return "", err
	}
	if err := encoder.Close(); err != nil {
		return "", err
	}
	return builder.String(), nil
}
