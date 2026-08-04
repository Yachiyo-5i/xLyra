package admin

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/downloads"
)

func TestWithDownloadServiceReturnsCopyWithDownloadService(t *testing.T) {
	t.Parallel()

	base := Handler{}
	downloadService := downloads.NewService()

	got := base.WithDownloadService(downloadService)

	if got.downloads != downloadService {
		t.Fatalf("downloads service = %#v, want %#v", got.downloads, downloadService)
	}
	if base.downloads != nil {
		t.Fatalf("base handler downloads = %#v, want nil", base.downloads)
	}
}

func TestCurrentAdminActorReadsContextOrReturnsZeroValue(t *testing.T) {
	t.Parallel()

	req := adminTestRequest(http.MethodGet, "/api/v1/profile", "")
	if got := currentAdminActor(req); got != (auth.AdminActor{}) {
		t.Fatalf("actor without context = %#v, want zero value", got)
	}

	actor := auth.AdminActor{
		Type:          "access_token",
		AdminID:       uuid.New(),
		AccessTokenID: uuid.New(),
	}
	req = req.WithContext(auth.WithAdminActor(req.Context(), actor))
	if got := currentAdminActor(req); got != actor {
		t.Fatalf("actor = %#v, want %#v", got, actor)
	}
}

func TestAuditAdminMutationPassesThroughNonMutationWithAuthService(t *testing.T) {
	t.Parallel()

	handler := Handler{auth: &auth.Service{}}
	nextCalled := false
	wrapped := handler.AuditAdminMutation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusAccepted)
	}))
	req := adminTestRequest(http.MethodGet, "/api/v1/sites?debug=1", "")
	rec := adminPerform(wrapped.ServeHTTP, req)

	if !nextCalled || rec.Code != http.StatusAccepted {
		t.Fatalf("expected middleware to pass through, nextCalled=%v status=%d", nextCalled, rec.Code)
	}
}
