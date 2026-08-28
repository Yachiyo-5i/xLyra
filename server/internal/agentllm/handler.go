package agentllm

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/gateway"
	"xlyra/server/internal/httpx"
	"xlyra/server/internal/store"
)

const (
	DefaultTokenTTL    = 30 * time.Minute
	PendingRunWindow   = 30 * time.Second
	TokenReasonInitial = "initial"
)

type Handler struct {
	logger  *slog.Logger
	repo    agentRepository
	gateway gateway.Handler
	now     func() time.Time
}

type agentRepository interface {
	FindRun(ctx context.Context, input store.AgentRunInput) (store.AgentRun, error)
	CreatePending(ctx context.Context, input store.AgentRunInput, expiresAt time.Time) (store.AgentRun, error)
	Register(ctx context.Context, input store.AgentRunInput, now time.Time) (store.AgentRun, error)
	End(ctx context.Context, input store.AgentRunInput, now time.Time) error
	FindUsableToken(ctx context.Context, token string, now time.Time) (store.AgentLLMToken, error)
	FindRenewableToken(ctx context.Context, token string, now time.Time) (store.AgentLLMToken, error)
	CreateToken(ctx context.Context, input store.AgentRunInput, token string, expiresAt time.Time) (store.AgentLLMToken, error)
	SupersedeTokens(ctx context.Context, input store.AgentRunInput, now time.Time) error
	EnsureInternalAPIKey(ctx context.Context) (store.APIKey, error)
	SyncInternalAPIKeyPolicy(ctx context.Context, sitePolicy string, siteIDs []uuid.UUID, modelPolicy string, siteModelIDs []uuid.UUID) (store.APIKey, error)
	ModelMemory(ctx context.Context) (string, map[string]string, error)
}

type credentialRequest struct {
	AgentInstanceID string `json:"agent_instance_id"`
	SessionID       string `json:"session_id"`
	RunID           string `json:"run_id"`
	Model           string `json:"model"`
	Reason          string `json:"reason"`
}

type runRequest struct {
	AgentInstanceID string `json:"agent_instance_id"`
	SessionID       string `json:"session_id"`
	RunID           string `json:"run_id"`
	Model           string `json:"model"`
}

type credentialResponse struct {
	Active    bool   `json:"active"`
	Token     string `json:"token,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

func NewHandler(logger *slog.Logger, db *store.Store, gatewayHandler gateway.Handler) Handler {
	return Handler{logger: logger, repo: store.NewAgentRepository(db.DB()), gateway: gatewayHandler, now: time.Now}
}

func (h Handler) Credential(w http.ResponseWriter, r *http.Request) {
	var input credentialRequest
	if err := decode(r, &input); err != nil {
		h.fail(w, r, http.StatusBadRequest, "invalid_request", "invalid credential request")
		return
	}
	if !validIdentifiers(input.AgentInstanceID, input.SessionID, input.RunID, input.Model) {
		h.fail(w, r, http.StatusBadRequest, "invalid_request", "agent_instance_id, session_id, run_id and model are required")
		return
	}
	// Only the initial issuance is accepted here. Rotation requires the current
	// token via the renew endpoint; anything else is a protocol violation.
	if input.Reason != "" && input.Reason != TokenReasonInitial {
		h.fail(w, r, http.StatusBadRequest, "invalid_request", "unsupported credential reason")
		return
	}
	now := h.now()
	runInput := store.AgentRunInput{AgentInstanceID: input.AgentInstanceID, SessionID: input.SessionID, RunID: input.RunID, Model: input.Model}
	run, err := h.repo.FindRun(r.Context(), runInput)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		run, err = h.repo.CreatePending(r.Context(), runInput, now.Add(PendingRunWindow))
	}
	if err != nil {
		h.fail(w, r, http.StatusInternalServerError, "agent_credential_unavailable", "unable to resolve agent run")
		return
	}
	if run.AgentInstanceID != input.AgentInstanceID || run.Model != input.Model || run.Status == store.AgentRunEnded || (run.Status == store.AgentRunPending && (run.PendingExpiresAt == nil || !run.PendingExpiresAt.After(now))) {
		h.writeCredential(w, credentialResponse{Active: false})
		return
	}
	h.issueToken(w, r, runInput, now)
}

// Renew rotates a run credential for an authenticated holder: the current live
// token is the proof of possession, so an expired or stolen credential cannot
// be exchanged for a fresh one. Callers are expected to renew proactively
// using expires_at, never in reaction to a 401 from the LLM entrypoints.
func (h Handler) Renew(w http.ResponseWriter, r *http.Request) {
	token := bearer(r.Header.Get("Authorization"))
	if token == "" {
		h.fail(w, r, http.StatusUnauthorized, "unauthorized", "temporary agent credential is required")
		return
	}
	now := h.now()
	item, err := h.repo.FindRenewableToken(r.Context(), token, now)
	if err != nil {
		h.fail(w, r, http.StatusUnauthorized, "unauthorized", "temporary agent credential is not renewable")
		return
	}
	runInput := store.AgentRunInput{AgentInstanceID: item.AgentInstanceID, SessionID: item.SessionID, RunID: item.RunID, Model: item.Model}
	h.issueToken(w, r, runInput, now)
}

func (h Handler) issueToken(w http.ResponseWriter, r *http.Request, runInput store.AgentRunInput, now time.Time) {
	// Every issuance displaces previous tokens: they die after the supersede
	// grace period, so a leaked credential cannot outlive the next rotation.
	if err := h.repo.SupersedeTokens(r.Context(), runInput, now); err != nil {
		h.fail(w, r, http.StatusInternalServerError, "agent_credential_unavailable", "unable to rotate agent credential")
		return
	}
	value, err := newToken()
	if err != nil {
		h.fail(w, r, http.StatusInternalServerError, "agent_credential_unavailable", "unable to create agent credential")
		return
	}
	expiresAt := now.Add(DefaultTokenTTL)
	if _, err := h.repo.CreateToken(r.Context(), runInput, value, expiresAt); err != nil {
		h.fail(w, r, http.StatusInternalServerError, "agent_credential_unavailable", "unable to persist agent credential")
		return
	}
	h.writeCredential(w, credentialResponse{Active: true, Token: value, ExpiresAt: expiresAt.UTC().Format(time.RFC3339)})
}

func (h Handler) Register(w http.ResponseWriter, r *http.Request) {
	h.runMutation(w, r, false)
}

func (h Handler) End(w http.ResponseWriter, r *http.Request) {
	h.runMutation(w, r, true)
}

func (h Handler) runMutation(w http.ResponseWriter, r *http.Request, ending bool) {
	var input runRequest
	if err := decode(r, &input); err != nil || !validIdentifiers(input.AgentInstanceID, input.SessionID, input.RunID, input.Model) {
		h.fail(w, r, http.StatusBadRequest, "invalid_request", "agent_instance_id, session_id, run_id and model are required")
		return
	}
	runInput := store.AgentRunInput{AgentInstanceID: input.AgentInstanceID, SessionID: input.SessionID, RunID: input.RunID, Model: input.Model}
	var err error
	if ending {
		err = h.repo.End(r.Context(), runInput, h.now())
	} else {
		_, err = h.repo.Register(r.Context(), runInput, h.now())
	}
	if err != nil {
		h.fail(w, r, http.StatusConflict, "agent_run_conflict", "agent run identity or state conflict")
		return
	}
	h.writeCredential(w, map[string]any{"ok": true})
}

// ModelMemory serves the admin UI: last-used model overall and per session,
// so the workspace can default to what the user actually used last.
func (h Handler) ModelMemory(w http.ResponseWriter, r *http.Request) {
	defaultModel, bySession, err := h.repo.ModelMemory(r.Context())
	if err != nil {
		h.fail(w, r, http.StatusInternalServerError, "agent_model_memory_unavailable", "unable to load agent model memory")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"data": map[string]any{"default_model": defaultModel, "sessions": bySession}})
}

func (h Handler) Responses(w http.ResponseWriter, r *http.Request) {
	h.llm(w, r, h.gateway.Responses)
}

func (h Handler) Messages(w http.ResponseWriter, r *http.Request) {
	h.llm(w, r, h.gateway.Messages)
}

func (h Handler) Models(w http.ResponseWriter, r *http.Request) {
	h.llm(w, r, h.gateway.Models)
}

func (h Handler) llm(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	token := bearer(r.Header.Get("Authorization"))
	if token == "" {
		h.fail(w, r, http.StatusUnauthorized, "unauthorized", "temporary agent credential is required")
		return
	}
	item, err := h.repo.FindUsableToken(r.Context(), token, h.now())
	if err != nil {
		h.fail(w, r, http.StatusUnauthorized, "unauthorized", "temporary agent credential is invalid")
		return
	}
	if requestedModel, ok := requestModel(r); ok && requestedModel != item.Model {
		h.fail(w, r, http.StatusForbidden, "agent_model_mismatch", "request model does not match the temporary agent credential")
		return
	}
	apiKey, err := h.repo.EnsureInternalAPIKey(r.Context())
	if err != nil {
		h.fail(w, r, http.StatusServiceUnavailable, "gateway_unavailable", "internal agent gateway is not available")
		return
	}
	ctx := auth.WithAPIKey(r.Context(), apiKey)
	ctx = gateway.WithInternalRequestMetadata(ctx, map[string]any{
		"agent_instance_id": item.AgentInstanceID,
		"agent_session_id":  item.SessionID,
		"agent_run_id":      item.RunID,
		"agent_transport":   "xlyra-agent",
	})
	next(w, r.WithContext(ctx))
}

func requestModel(r *http.Request) (string, bool) {
	if r.Body == nil || r.Method != http.MethodPost {
		return "", false
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return "", false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	var payload struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(body, &payload) != nil || strings.TrimSpace(payload.Model) == "" {
		return "", false
	}
	return strings.TrimSpace(payload.Model), true
}

// AccessPolicy mirrors the agent allowlist settings onto the internal gateway
// key. IDs arrive as strings (from the config file); invalid entries are
// skipped rather than failing the whole sync.
type AccessPolicy struct {
	SitePolicy   string
	ModelPolicy  string
	SiteIDs      []string
	SiteModelIDs []string
}

// SyncAccessPolicy applies the agent site/model allowlist to the internal API
// key and refreshes the gateway models cache for it.
func (h Handler) SyncAccessPolicy(ctx context.Context, policy AccessPolicy) error {
	key, err := h.repo.SyncInternalAPIKeyPolicy(ctx, policy.SitePolicy, parseUUIDs(policy.SiteIDs), policy.ModelPolicy, parseUUIDs(policy.SiteModelIDs))
	if err != nil {
		return err
	}
	h.gateway.InvalidateModelsCacheForAPIKey(key.ID)
	return nil
}

func parseUUIDs(values []string) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(values))
	seen := make(map[uuid.UUID]struct{}, len(values))
	for _, value := range values {
		id, err := uuid.Parse(strings.TrimSpace(value))
		if err != nil || id == uuid.Nil {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func (h Handler) writeCredential(w http.ResponseWriter, payload any) {
	httpx.JSON(w, http.StatusOK, payload)
}

func (h Handler) fail(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	httpx.Error(w, r, status, code, message)
}

func decode(r *http.Request, target any) error {
	return httpx.DecodeJSONBody(r, target)
}

func validIdentifiers(values ...string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == "" || len(value) > 256 {
			return false
		}
	}
	return true
}

func bearer(value string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(value, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(value, prefix))
}

func newToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
