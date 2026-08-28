package agentllm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

func TestValidIdentifiers(t *testing.T) {
	if !validIdentifiers("agent", "session", "run", "model") {
		t.Fatal("expected identifiers to be valid")
	}
	if validIdentifiers("agent", "", "run", "model") {
		t.Fatal("expected empty identifier to be invalid")
	}
}

type fakeAgentRepository struct {
	run            store.AgentRun
	runErr         error
	renewableToken store.AgentLLMToken
	renewableErr   error

	supersededInput *store.AgentRunInput
	createdInput    *store.AgentRunInput
	createdExpiry   time.Time
}

func (f *fakeAgentRepository) FindRun(context.Context, store.AgentRunInput) (store.AgentRun, error) {
	return f.run, f.runErr
}

func (f *fakeAgentRepository) CreatePending(_ context.Context, input store.AgentRunInput, expiresAt time.Time) (store.AgentRun, error) {
	return store.AgentRun{AgentInstanceID: input.AgentInstanceID, SessionID: input.SessionID, RunID: input.RunID, Model: input.Model, Status: store.AgentRunPending, PendingExpiresAt: &expiresAt}, nil
}

func (f *fakeAgentRepository) Register(context.Context, store.AgentRunInput, time.Time) (store.AgentRun, error) {
	return store.AgentRun{}, nil
}

func (f *fakeAgentRepository) End(context.Context, store.AgentRunInput, time.Time) error { return nil }

func (f *fakeAgentRepository) FindUsableToken(context.Context, string, time.Time) (store.AgentLLMToken, error) {
	return store.AgentLLMToken{}, gorm.ErrRecordNotFound
}

func (f *fakeAgentRepository) FindRenewableToken(context.Context, string, time.Time) (store.AgentLLMToken, error) {
	return f.renewableToken, f.renewableErr
}

func (f *fakeAgentRepository) CreateToken(_ context.Context, input store.AgentRunInput, _ string, expiresAt time.Time) (store.AgentLLMToken, error) {
	f.createdInput = &input
	f.createdExpiry = expiresAt
	return store.AgentLLMToken{}, nil
}

func (f *fakeAgentRepository) SupersedeTokens(_ context.Context, input store.AgentRunInput, _ time.Time) error {
	f.supersededInput = &input
	return nil
}

func (f *fakeAgentRepository) EnsureInternalAPIKey(context.Context) (store.APIKey, error) {
	return store.APIKey{}, errors.New("not implemented")
}

func (f *fakeAgentRepository) SyncInternalAPIKeyPolicy(context.Context, string, []uuid.UUID, string, []uuid.UUID) (store.APIKey, error) {
	return store.APIKey{}, errors.New("not implemented")
}

func (f *fakeAgentRepository) ModelMemory(context.Context) (string, map[string]string, error) {
	return "", map[string]string{}, nil
}

func newTestHandler(repo agentRepository) Handler {
	return Handler{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), repo: repo, now: time.Now}
}

func credentialBody(reason string) *strings.Reader {
	return strings.NewReader(`{"agent_instance_id":"agent-1","session_id":"sess-1","run_id":"run-1","model":"gpt-5","reason":"` + reason + `"}`)
}

func decodeCredentialResponse(t *testing.T, rec *httptest.ResponseRecorder) credentialResponse {
	t.Helper()
	var payload credentialResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode credential response: %v", err)
	}
	return payload
}

func TestCredentialRejectsUnknownReason(t *testing.T) {
	repo := &fakeAgentRepository{}
	handler := newTestHandler(repo)
	req := httptest.NewRequest(http.MethodPost, "/internal/agent-llm/credential", credentialBody("unauthorized"))
	rec := httptest.NewRecorder()
	handler.Credential(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if repo.createdInput != nil {
		t.Fatal("no token should be created for an unknown reason")
	}
}

func TestCredentialInitialCreatesPendingAndIssuesToken(t *testing.T) {
	repo := &fakeAgentRepository{runErr: gorm.ErrRecordNotFound}
	handler := newTestHandler(repo)
	req := httptest.NewRequest(http.MethodPost, "/internal/agent-llm/credential", credentialBody("initial"))
	rec := httptest.NewRecorder()
	handler.Credential(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	payload := decodeCredentialResponse(t, rec)
	if !payload.Active || payload.Token == "" || payload.ExpiresAt == "" {
		t.Fatalf("expected active credential, got %+v", payload)
	}
	if repo.createdInput == nil || repo.createdInput.SessionID != "sess-1" || repo.createdInput.RunID != "run-1" {
		t.Fatalf("token created with unexpected identity: %+v", repo.createdInput)
	}
	if repo.supersededInput == nil {
		t.Fatal("issuance must supersede previous tokens")
	}
}

func TestCredentialEndedRunIsInactive(t *testing.T) {
	repo := &fakeAgentRepository{run: store.AgentRun{AgentInstanceID: "agent-1", SessionID: "sess-1", RunID: "run-1", Model: "gpt-5", Status: store.AgentRunEnded}}
	handler := newTestHandler(repo)
	req := httptest.NewRequest(http.MethodPost, "/internal/agent-llm/credential", credentialBody("initial"))
	rec := httptest.NewRecorder()
	handler.Credential(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if payload := decodeCredentialResponse(t, rec); payload.Active {
		t.Fatal("ended run must not receive a credential")
	}
	if repo.createdInput != nil {
		t.Fatal("no token should be created for an ended run")
	}
}

func TestRenewRequiresBearerToken(t *testing.T) {
	handler := newTestHandler(&fakeAgentRepository{})
	req := httptest.NewRequest(http.MethodPost, "/internal/agent-llm/credential/renew", nil)
	rec := httptest.NewRecorder()
	handler.Renew(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestRenewRejectsUnrenewableToken(t *testing.T) {
	repo := &fakeAgentRepository{renewableErr: gorm.ErrRecordNotFound}
	handler := newTestHandler(repo)
	req := httptest.NewRequest(http.MethodPost, "/internal/agent-llm/credential/renew", nil)
	req.Header.Set("Authorization", "Bearer stale-token")
	rec := httptest.NewRecorder()
	handler.Renew(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if repo.createdInput != nil {
		t.Fatal("no token should be created for a non-renewable credential")
	}
}

func TestRenewRotatesUsingTokenIdentity(t *testing.T) {
	repo := &fakeAgentRepository{renewableToken: store.AgentLLMToken{
		AgentInstanceID: "agent-9",
		SessionID:       "sess-9",
		RunID:           "run-9",
		Model:           "claude-opus",
		ExpiresAt:       time.Now().Add(10 * time.Minute),
	}}
	handler := newTestHandler(repo)
	req := httptest.NewRequest(http.MethodPost, "/internal/agent-llm/credential/renew", nil)
	req.Header.Set("Authorization", "Bearer live-token")
	rec := httptest.NewRecorder()
	handler.Renew(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	payload := decodeCredentialResponse(t, rec)
	if !payload.Active || payload.Token == "" {
		t.Fatalf("expected rotated credential, got %+v", payload)
	}
	if repo.supersededInput == nil || repo.supersededInput.SessionID != "sess-9" || repo.supersededInput.RunID != "run-9" || repo.supersededInput.Model != "claude-opus" {
		t.Fatalf("supersede must use the token identity, got %+v", repo.supersededInput)
	}
	if repo.createdInput == nil || repo.createdInput.AgentInstanceID != "agent-9" {
		t.Fatalf("token created with unexpected identity: %+v", repo.createdInput)
	}
	if !repo.createdExpiry.After(time.Now().Add(DefaultTokenTTL - time.Minute)) {
		t.Fatal("new token must carry a fresh TTL")
	}
}

func TestParseUUIDsSkipsInvalidAndDuplicates(t *testing.T) {
	id := uuid.New()
	ids := parseUUIDs([]string{id.String(), "not-a-uuid", " ", id.String(), uuid.Nil.String()})
	if len(ids) != 1 || ids[0] != id {
		t.Fatalf("parseUUIDs = %v, want [%v]", ids, id)
	}
}
