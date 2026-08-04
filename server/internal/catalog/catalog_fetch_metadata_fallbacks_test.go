package catalog

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"xlyra/server/internal/store"
)

func TestFetchCatalogReturnsTransportError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("network unavailable")
	service := &SyncService{client: &http.Client{Transport: catalogRoundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return nil, wantErr
	})}}

	if _, err := service.fetchCatalog(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("fetchCatalog error = %v, want %v", err, wantErr)
	}
}

func TestCatalogMetadataHelpersUseNameAndCategoryFallbacks(t *testing.T) {
	t.Parallel()

	if got := displayNameFromModel(store.SiteModel{}); got != "" {
		t.Fatalf("empty displayNameFromModel = %q, want empty", got)
	}

	categoryCases := map[string]string{
		"vendor-audio-image-embedding": "embedding",
		"vendor-audio-image":           "audio",
		"vendor-vedio-generator":       "video",
	}
	for input, want := range categoryCases {
		if got := InferCategory(input); got != want {
			t.Fatalf("InferCategory(%q) = %q, want %q", input, got, want)
		}
	}

	if got := CanonicalModelKeyFromUpstream("vendor-search-business"); got != "vendor-search-business" {
		t.Fatalf("noise-only suffix should be retained without a known family token, got %q", got)
	}
}
