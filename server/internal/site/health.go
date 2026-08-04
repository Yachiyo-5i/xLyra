package site

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/store"
	"xlyra/server/internal/upstream"
)

const siteHealthRecentLimit = 20
const siteHealthCooldownDuration = 15 * time.Minute
const siteHealthDataRetention = 24 * time.Hour

type HealthResult struct {
	Site     store.Site
	State    store.SiteHealthState
	Snapshot store.HealthSnapshot
	Recent   []store.HealthSnapshot
}

type HealthHourlyBucket struct {
	BucketStart  time.Time
	Status       string
	SuccessCount int
	FailureCount int
	TotalCount   int
}

func (s *Service) CheckSiteHealth(ctx context.Context, siteID uuid.UUID, source string) (HealthResult, error) {
	item, err := s.Get(ctx, siteID)
	if err != nil {
		return HealthResult{}, err
	}

	if strings.TrimSpace(source) == "" {
		source = "manual"
	}

	check := s.runSiteHealthCheck(ctx, item)
	now := time.Now()
	latencyMS := int64(check.latency / time.Millisecond)
	metadata, _ := json.Marshal(check.metadata)

	repo := store.NewHealthRepository(s.db.DB())
	snapshot, err := repo.CreateSnapshot(ctx, store.CreateHealthSnapshotParams{
		SiteID:       item.ID,
		SiteModelID:  nil,
		Scope:        "site",
		Source:       source,
		Endpoint:     check.endpoint,
		Method:       check.method,
		Success:      check.success,
		StatusCode:   nil,
		LatencyMS:    latencyMS,
		ErrorType:    nullableHealthString(check.errorType),
		ErrorMessage: nullableHealthString(check.message),
		CheckedAt:    now,
		Metadata:     metadata,
	})
	if err != nil {
		return HealthResult{}, err
	}

	recent, err := repo.ListSiteSnapshots(ctx, item.ID, siteHealthRecentLimit)
	if err != nil {
		return HealthResult{}, err
	}

	stateRecent := s.siteHealthSnapshotsForState(item, recent)
	state, err := repo.UpsertSiteState(ctx, buildSiteHealthStateParams(item.ID, snapshot, stateRecent))
	if err != nil {
		return HealthResult{}, err
	}

	if err := s.syncSiteHealthCooldown(ctx, item.ID, state, snapshot.CheckedAt, source); err != nil {
		return HealthResult{}, err
	}

	return HealthResult{
		Site:     item,
		State:    state,
		Snapshot: snapshot,
		Recent:   recent,
	}, nil
}

func (s *Service) siteHealthSnapshotsForState(item store.Site, recent []store.HealthSnapshot) []store.HealthSnapshot {
	module, ok := s.adapters.ModuleForSiteType(item.SiteType)
	if !ok {
		return recent
	}
	if _, ok := adapter.AsHealthProbe(module); !ok {
		return recent
	}
	filtered := make([]store.HealthSnapshot, 0, len(recent))
	for _, snapshot := range recent {
		if legacyUnsupportedHealthSnapshot(item, snapshot) {
			continue
		}
		filtered = append(filtered, snapshot)
	}
	return filtered
}

func legacyUnsupportedHealthSnapshot(item store.Site, snapshot store.HealthSnapshot) bool {
	if !snapshot.ErrorType.Valid || snapshot.ErrorType.String != "unsupported_health_check" {
		return false
	}
	if snapshot.Endpoint != "detect" || snapshot.Method != "GET" {
		return false
	}
	metadata := map[string]any{}
	if err := json.Unmarshal(snapshot.Metadata, &metadata); err != nil {
		return false
	}
	siteType, _ := metadata["site_type"].(string)
	return strings.EqualFold(strings.TrimSpace(siteType), strings.TrimSpace(item.SiteType))
}

func (s *Service) SiteHealth(ctx context.Context, siteID uuid.UUID) (HealthResult, error) {
	item, err := s.Get(ctx, siteID)
	if err != nil {
		return HealthResult{}, err
	}

	repo := store.NewHealthRepository(s.db.DB())
	state, err := repo.GetSiteState(ctx, siteID)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return HealthResult{}, err
		}
		state = unknownSiteHealthState(siteID)
	}

	recent, err := repo.ListSiteSnapshots(ctx, siteID, siteHealthRecentLimit)
	if err != nil {
		return HealthResult{}, err
	}

	return HealthResult{
		Site:   item,
		State:  state,
		Recent: recent,
	}, nil
}

func (s *Service) SiteHealthStates(ctx context.Context) (map[uuid.UUID]store.SiteHealthState, error) {
	return store.NewHealthRepository(s.db.DB()).ListSiteStates(ctx)
}

func (s *Service) SiteHealthHistory(ctx context.Context, siteID uuid.UUID, limit int, source string) ([]store.HealthSnapshot, error) {
	if _, err := s.Get(ctx, siteID); err != nil {
		return nil, err
	}

	return store.NewHealthRepository(s.db.DB()).ListSiteSnapshotsBySource(ctx, siteID, limit, source)
}

func (s *Service) SiteHealthHourly(ctx context.Context, siteID uuid.UUID, hours int) ([]HealthHourlyBucket, error) {
	if _, err := s.Get(ctx, siteID); err != nil {
		return nil, err
	}

	if hours <= 0 {
		hours = 24
	}
	if hours > 168 {
		hours = 168
	}

	now := s.timeZone.StartOfHour(time.Now())
	start := now.Add(-time.Duration(hours-1) * time.Hour)
	rows, err := store.NewHealthRepository(s.db.DB()).ListSiteHourlyBuckets(ctx, siteID, start)
	if err != nil {
		return nil, err
	}

	byHour := make(map[time.Time]store.SiteHealthHourlyBucket, len(rows))
	for _, item := range rows {
		byHour[item.BucketTime] = item
	}

	result := make([]HealthHourlyBucket, 0, hours)
	for bucket := start; !bucket.After(now); bucket = bucket.Add(time.Hour) {
		item, ok := byHour[bucket]
		if !ok {
			result = append(result, HealthHourlyBucket{
				BucketStart: bucket,
				Status:      "idle",
			})
			continue
		}

		status := "degraded"
		switch {
		case item.TotalCount == 0:
			status = "idle"
		case item.SuccessCount == item.TotalCount:
			status = "healthy"
		case item.SuccessCount == 0:
			status = "unhealthy"
		}

		result = append(result, HealthHourlyBucket{
			BucketStart:  bucket,
			Status:       status,
			SuccessCount: item.SuccessCount,
			FailureCount: item.FailureCount,
			TotalCount:   item.TotalCount,
		})
	}

	return result, nil
}

func (s *Service) CleanupExpiredHealthData(ctx context.Context, now time.Time) error {
	if now.IsZero() {
		now = time.Now()
	}
	cutoff := now.Add(-siteHealthDataRetention)
	return s.db.WithinTx(ctx, func(tx store.Tx) error {
		repo := store.NewHealthRepository(tx)
		if err := repo.DeleteSiteStatesBefore(ctx, cutoff); err != nil {
			return err
		}
		if err := repo.DeleteSnapshotsBefore(ctx, cutoff); err != nil {
			return err
		}
		return nil
	})
}

type siteHealthCheck struct {
	success   bool
	endpoint  string
	method    string
	errorType string
	message   string
	latency   time.Duration
	metadata  map[string]any
}

func (s *Service) runSiteHealthCheck(ctx context.Context, item store.Site) siteHealthCheck {
	start := time.Now()
	check := siteHealthCheck{
		endpoint: "detect",
		method:   "GET",
		metadata: map[string]any{
			"site_type": item.SiteType,
		},
	}

	module, ok := s.adapters.ModuleForSiteType(item.SiteType)
	if !ok {
		check.latency = time.Since(start)
		check.errorType = "unsupported_site_type"
		check.message = fmt.Sprintf("unsupported site_type %q", item.SiteType)
		return check
	}

	if probe, ok := adapter.AsHealthProbe(module); ok {
		return s.runHealthProbe(ctx, item, probe, start, check)
	}

	if detector, ok := adapter.AsDetector(module); ok {
		check.endpoint = "GET /api/status"
		result, err := detector.Detect(ctx, item.BaseURL)
		check.latency = time.Since(start)
		check.metadata["detect"] = result
		if err != nil {
			check.errorType = "upstream_unreachable"
			check.message = err.Error()
			return check
		}
		if !result.Matched {
			check.errorType = "site_type_mismatch"
			check.message = "site detection did not match"
			return check
		}
		_, supportsCredentialValidation := adapter.AsCredentialValidator(module)
		if CredentialTypeForSiteType(item.SiteType) != "api_key" || !supportsCredentialValidation {
			check.success = true
			check.message = "ok"
			return check
		}
	}

	validator, ok := adapter.AsCredentialValidator(module)
	if normalizeSiteType(item.SiteType) == "codex" || normalizeSiteType(item.SiteType) == "antigravity" || normalizeSiteType(item.SiteType) == "claude_code" {
		systemValidator, ok := adapter.AsSystemCredentialValidator(module)
		if !ok {
			check.latency = time.Since(start)
			check.errorType = "unsupported_health_check"
			check.message = fmt.Sprintf("site_type %q does not support oauth health checks", item.SiteType)
			return check
		}
		check.endpoint = "oauth model/quota validation"
		auth, err := s.SystemAuth(ctx, item.ID)
		if err != nil {
			check.latency = time.Since(start)
			check.errorType = "missing_credential"
			check.message = err.Error()
			return check
		}
		if err := systemValidator.ValidateSystemCredentials(ctx, s.toAdapterSite(ctx, item), auth); err != nil {
			check.latency = time.Since(start)
			check.errorType = "validation_failed"
			check.message = err.Error()
			return check
		}

		check.latency = time.Since(start)
		check.success = true
		check.message = "ok"
		return check
	}

	if !ok {
		check.latency = time.Since(start)
		check.errorType = "unsupported_health_check"
		check.message = fmt.Sprintf("site_type %q does not support site health checks", item.SiteType)
		return check
	}

	if normalizeSiteType(item.SiteType) == "xlyra" {
		if systemValidator, ok := adapter.AsSystemCredentialValidator(module); ok {
			if auth, err := s.SystemAuth(ctx, item.ID); err == nil && strings.TrimSpace(auth.AccessToken) != "" {
				check.endpoint = "GET /api/v1/profile"
				if err := systemValidator.ValidateSystemCredentials(ctx, s.toAdapterSite(ctx, item), auth); err != nil {
					check.latency = time.Since(start)
					check.errorType = "validation_failed"
					check.message = err.Error()
					return check
				}
				check.latency = time.Since(start)
				check.success = true
				check.message = "ok"
				return check
			}
		}
	}

	check.endpoint = "GET /v1/models"
	if CredentialTypeForSiteType(item.SiteType) == "api_key" {
		return s.runAPIKeySiteHealthCheck(ctx, item, validator, start, check)
	}
	_, apiKeyCredential, err := s.siteWithAPIKeyCredential(ctx, item.ID)
	if err != nil {
		check.latency = time.Since(start)
		check.errorType = "missing_credential"
		check.message = err.Error()
		return check
	}
	apiKey := apiKeyCredential.Secret
	if err := validator.ValidateCredentials(ctx, s.toAdapterSite(ctx, item), apiKey); err != nil {
		if failure := upstream.ClassifyError(err); failure.Limited() {
			check.latency = time.Since(start)
			check.success = true
			check.message = "ok"
			check.metadata["credential_limited"] = healthFailureMetadata(failure)
			return check
		}
		check.latency = time.Since(start)
		check.errorType = "validation_failed"
		check.message = err.Error()
		return check
	}

	check.latency = time.Since(start)
	check.success = true
	check.message = "ok"
	return check
}

func (s *Service) runHealthProbe(ctx context.Context, item store.Site, probe adapter.HealthProbe, start time.Time, check siteHealthCheck) siteHealthCheck {
	switch probe.Scope() {
	case adapter.HealthProbeCredentialScope:
		return s.runCredentialHealthProbe(ctx, item, probe, start, check)
	case adapter.HealthProbeSiteScope:
		return s.runSiteModelHealthProbe(ctx, item, probe, start, check)
	default:
		check.latency = time.Since(start)
		check.errorType = "unsupported_health_probe"
		check.message = fmt.Sprintf("health probe scope %q is not supported", probe.Scope())
		return check
	}
}

func (s *Service) runSiteModelHealthProbe(ctx context.Context, item store.Site, probe adapter.HealthProbe, start time.Time, check siteHealthCheck) siteHealthCheck {
	check.endpoint = "GET /v1/models"
	check.metadata["probe"] = "models"
	models, err := probe.ProbeHealth(ctx, s.toAdapterSite(ctx, item), "")
	check.latency = time.Since(start)
	if err != nil {
		check.errorType = "health_probe_failed"
		check.message = err.Error()
		return check
	}
	check.metadata["model_count"] = len(models)
	if len(models) == 0 {
		check.errorType = "health_probe_empty_models"
		check.message = fmt.Sprintf("site_type %q returned no models", item.SiteType)
		return check
	}
	check.success = true
	check.message = fmt.Sprintf("ok (%d models)", len(models))
	return check
}

func (s *Service) runAPIKeySiteHealthCheck(ctx context.Context, item store.Site, validator adapter.CredentialValidator, start time.Time, check siteHealthCheck) siteHealthCheck {
	credentials, err := store.NewSiteCredentialRepository(s.db.DB()).ListBySite(ctx, item.ID)
	if err != nil {
		check.latency = time.Since(start)
		check.errorType = "missing_credential"
		check.message = err.Error()
		return check
	}
	states, err := store.NewSiteAPIKeyStateRepository(s.db.DB()).ListBySite(ctx, item.ID)
	if err != nil {
		check.latency = time.Since(start)
		check.errorType = "missing_credential_state"
		check.message = err.Error()
		return check
	}
	statesByCredentialID := make(map[uuid.UUID]store.SiteAPIKeyState, len(states))
	for _, state := range states {
		statesByCredentialID[state.SiteCredentialID] = state
	}
	items := make([]store.GatewayCredential, 0, len(credentials))
	for _, credential := range credentials {
		if credential.CredentialType != defaultCredentialType && !strings.HasPrefix(credential.CredentialType, defaultCredentialType+":") {
			continue
		}
		if credential.CredentialType == defaultCredentialType && hasTokenScopedCredentials(credentials) {
			continue
		}
		state := statesByCredentialID[credential.ID]
		if store.CredentialUsable(credential) && store.CredentialStateUsableForCredential(credential, state) {
			items = append(items, store.GatewayCredential{Credential: credential, State: state, GroupName: state.GroupName})
		}
	}
	store.SortGatewayCredentials(items)
	if len(items) == 0 {
		check.latency = time.Since(start)
		check.errorType = "missing_credential"
		check.message = "no usable api key credential"
		return check
	}

	attempts := make([]map[string]any, 0, len(items))
	lastError := ""
	limitedCount := 0
	for _, gatewayCredential := range items {
		credential := gatewayCredential.Credential
		attempt := map[string]any{
			"id":               credential.ID.String(),
			"name":             store.SiteCredentialDisplayName(credential, gatewayCredential.State),
			"masked_secret":    credential.MaskedSecret,
			"routing_priority": store.SiteCredentialRoutingPriority(credential),
		}
		secret, decryptErr := s.credentials.Decrypt(credential.EncryptedSecret)
		if decryptErr != nil {
			lastError = decryptErr.Error()
			attempt["status"] = "failed"
			attempt["error"] = lastError
			attempts = append(attempts, attempt)
			continue
		}
		meta := map[string]any{}
		if len(credential.Meta) > 0 {
			_ = json.Unmarshal(credential.Meta, &meta)
		}
		secret = secretFromCredentialMeta(secret, meta)
		if strings.TrimSpace(secret) == "" {
			lastError = "api key is empty"
			attempt["status"] = "failed"
			attempt["error"] = lastError
			attempts = append(attempts, attempt)
			continue
		}
		if validateErr := validator.ValidateCredentials(ctx, s.toAdapterSite(ctx, item), secret); validateErr != nil {
			lastError = validateErr.Error()
			failure := upstream.ClassifyError(validateErr)
			if failure.Limited() {
				limitedCount++
				attempt["status"] = "limited"
				attempt["failure"] = healthFailureMetadata(failure)
			} else {
				attempt["status"] = "failed"
			}
			attempt["error"] = lastError
			attempts = append(attempts, attempt)
			continue
		}
		attempt["status"] = "ok"
		attempts = append(attempts, attempt)
		check.latency = time.Since(start)
		check.success = true
		check.message = "ok"
		check.metadata["credential"] = attempt
		check.metadata["credential_attempts"] = attempts
		return check
	}
	if limitedCount > 0 {
		check.latency = time.Since(start)
		check.success = true
		check.message = "ok"
		check.metadata["credential_attempts"] = attempts
		return check
	}

	check.latency = time.Since(start)
	check.errorType = "validation_failed"
	check.message = lastError
	check.metadata["credential_attempts"] = attempts
	return check
}

func healthFailureMetadata(failure upstream.Failure) map[string]any {
	var resetAt any
	if !failure.ResetAt.IsZero() {
		resetAt = failure.ResetAt.Format(time.RFC3339)
	}
	return map[string]any{
		"class":               failure.Class,
		"status_code":         failure.StatusCode,
		"code":                failure.Code,
		"scope":               failure.Scope,
		"retry_after_seconds": failure.RetryAfterSeconds,
		"reset_at":            resetAt,
	}
}

func buildSiteHealthStateParams(siteID uuid.UUID, snapshot store.HealthSnapshot, recent []store.HealthSnapshot) store.UpsertSiteHealthStateParams {
	successCount := 0
	latencyCount := 0
	latencyTotal := int64(0)
	consecutiveFailures := 0
	for index, item := range recent {
		if item.Success {
			successCount++
		}
		if item.LatencyMS.Valid {
			latencyCount++
			latencyTotal += item.LatencyMS.Int64
		}
		if index == consecutiveFailures && !item.Success {
			consecutiveFailures++
		}
	}

	var successRate any
	if len(recent) > 0 {
		successRate = float64(successCount) / float64(len(recent))
	}

	var avgLatency any
	if latencyCount > 0 {
		avgLatency = latencyTotal / int64(latencyCount)
	}

	status := "unknown"
	if len(recent) > 0 {
		rate, _ := successRate.(float64)
		switch {
		case snapshot.Success && rate >= 0.8:
			status = "healthy"
		case snapshot.Success:
			status = "degraded"
		case consecutiveFailures >= 3:
			status = "unhealthy"
		default:
			status = "degraded"
		}
	}

	var lastSuccessAt any
	var lastFailureAt any
	if snapshot.Success {
		lastSuccessAt = snapshot.CheckedAt
	} else {
		lastFailureAt = snapshot.CheckedAt
	}

	message := "ok"
	if snapshot.ErrorMessage.Valid {
		message = snapshot.ErrorMessage.String
	}
	metadata, _ := json.Marshal(map[string]any{
		"recent_window": len(recent),
	})

	return store.UpsertSiteHealthStateParams{
		SiteID:              siteID,
		Status:              status,
		LastSnapshotID:      snapshot.ID,
		LastSuccessAt:       lastSuccessAt,
		LastFailureAt:       lastFailureAt,
		ConsecutiveFailures: consecutiveFailures,
		RecentSuccessRate:   successRate,
		RecentAvgLatencyMS:  avgLatency,
		CheckedAt:           snapshot.CheckedAt,
		Message:             nullableHealthString(message),
		Metadata:            metadata,
	}
}

func (s *Service) syncSiteHealthCooldown(ctx context.Context, siteID uuid.UUID, state store.SiteHealthState, checkedAt time.Time, source string) error {
	repo := store.NewRouteCooldownRepository(s.db.DB())
	if state.Status == "unhealthy" {
		metadata, _ := json.Marshal(map[string]any{
			"status":     state.Status,
			"source":     source,
			"checked_at": s.timeZone.Format(checkedAt, time.RFC3339),
		})
		_, err := repo.Activate(ctx, store.ActivateRouteCooldownParams{
			SiteID:      siteID,
			SiteModelID: nil,
			Scope:       "site",
			Source:      "site_health",
			Reason:      "site health is unhealthy",
			ActiveUntil: checkedAt.Add(siteHealthCooldownDuration),
			Metadata:    metadata,
		})
		return err
	}

	return repo.ClearActive(ctx, siteID, nil, nil, "site_health")
}

func unknownSiteHealthState(siteID uuid.UUID) store.SiteHealthState {
	return store.SiteHealthState{
		SiteID:   siteID,
		Status:   "unknown",
		Metadata: []byte(`{}`),
	}
}

func nullableHealthString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
