package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

type Recorder struct {
	db       *store.Store
	logger   *slog.Logger
	timeZone config.TimeZone
}

type GatewayRequestRecord struct {
	RequestID                  string
	ParentRequestID            string
	APIKeyID                   uuid.UUID
	SiteID                     uuid.UUID
	CanonicalModelID           uuid.UUID
	SiteModelID                uuid.UUID
	Endpoint                   string
	StatusCode                 int
	Success                    bool
	ErrorType                  string
	LatencyMS                  int64
	UpstreamLatencyMS          int64
	FirstByteLatencyMS         int64
	PromptTokens               int
	CompletionTokens           int
	CachedTokens               int
	CacheWriteTokens           int
	CacheCreationInputTokens   int
	CacheCreation5mInputTokens int
	CacheCreation1hInputTokens int
	CacheWriteCost             *float64
	EstimatedCost              *float64
	Currency                   string
	UpstreamStatusCode         int
	UpstreamResponse           any
	ErrorMessage               string
	Metadata                   map[string]any
	SkipHealthSnapshot         bool
	Internal                   bool
	ParentRequestLogID         uuid.UUID
}

func NewRecorder(db *store.Store, logger *slog.Logger, timeZones ...config.TimeZone) Recorder {
	return Recorder{db: db, logger: logger, timeZone: recorderTimeZone(timeZones...)}
}

func (r Recorder) RecordGatewayRequest(ctx context.Context, record GatewayRequestRecord) (store.RequestLog, *store.UsageRecord, error) {
	if r.db == nil {
		return store.RequestLog{}, nil, fmt.Errorf("store is not initialized")
	}

	var requestLog store.RequestLog
	var usageRecord *store.UsageRecord
	var quotaOvershoot *store.APIKey
	err := r.db.WithinTx(ctx, func(tx store.Tx) error {
		metadata := record.Metadata
		if metadata == nil {
			metadata = map[string]any{}
		}
		metadata["upstream_status_code"] = record.UpstreamStatusCode
		metadata["upstream_response"] = record.UpstreamResponse
		metadataBytes, _ := json.Marshal(metadata)

		if record.EstimatedCost != nil && record.APIKeyID != uuid.Nil {
			updatedKey, err := store.NewAPIKeyRepository(tx).AddUsageAt(ctx, record.APIKeyID, *record.EstimatedCost, time.Now(), r.timeZone)
			if err != nil {
				return err
			}
			// Quota is a soft limit: concurrent in-flight requests can each pass
			// the auth-time gate before any deduction lands, so current total usage
			// may overshoot the limit. Once it does, the next auth is rejected. Flag
			// the request that crossed the limit so operators can see the overshoot.
			if updatedKey.QuotaExceeded() && updatedKey.EffectiveTotalQuotaUsed()-*record.EstimatedCost < updatedKey.QuotaLimit.Float64 {
				overshoot := updatedKey
				quotaOvershoot = &overshoot
			}
		}

		createdLog, err := store.NewRequestLogRepository(tx).Create(ctx, store.CreateRequestLogParams{
			RequestID:          record.RequestID,
			ParentRequestID:    nullableString(record.ParentRequestID),
			APIKeyID:           nullableUUID(record.APIKeyID),
			SiteID:             nullableUUID(record.SiteID),
			CanonicalModelID:   nullableUUID(record.CanonicalModelID),
			SiteModelID:        nullableUUID(record.SiteModelID),
			Endpoint:           stringValue(&record.Endpoint, gatewayEndpointChatCompletions),
			StatusCode:         record.StatusCode,
			Success:            record.Success,
			ErrorType:          nullableString(record.ErrorType),
			LatencyMS:          nullableInt64(record.LatencyMS),
			UpstreamLatencyMS:  nullableInt64(record.UpstreamLatencyMS),
			RequestTokens:      nullableInt(record.PromptTokens),
			ResponseTokens:     nullableInt(record.CompletionTokens),
			Metadata:           metadataBytes,
			Internal:           record.Internal,
			ParentRequestLogID: nullableUUID(record.ParentRequestLogID),
		})
		if err != nil {
			return err
		}
		requestLog = createdLog

		totalTokens := record.PromptTokens + record.CompletionTokens
		if totalTokens > 0 || record.EstimatedCost != nil {
			createdUsage, err := store.NewUsageRecordRepository(tx).Create(ctx, store.CreateUsageRecordParams{
				RequestLogID:               requestLog.ID,
				APIKeyID:                   nullableUUID(record.APIKeyID),
				SiteID:                     nullableUUID(record.SiteID),
				CanonicalModelID:           nullableUUID(record.CanonicalModelID),
				PromptTokens:               record.PromptTokens,
				CompletionTokens:           record.CompletionTokens,
				TotalTokens:                totalTokens,
				CachedTokens:               record.CachedTokens,
				CacheWriteTokens:           record.CacheWriteTokens,
				CacheCreationInputTokens:   record.CacheCreationInputTokens,
				CacheCreation5mInputTokens: record.CacheCreation5mInputTokens,
				CacheCreation1hInputTokens: record.CacheCreation1hInputTokens,
				CacheWriteCost:             nullableFloat(record.CacheWriteCost),
				EstimatedCost:              nullableFloat(record.EstimatedCost),
				Currency:                   record.Currency,
			})
			if err != nil {
				return err
			}
			usageRecord = &createdUsage
		}

		if record.SiteID != uuid.Nil && record.SiteModelID != uuid.Nil && !record.SkipHealthSnapshot {
			healthMetadata, _ := json.Marshal(map[string]any{
				"request_id":           record.RequestID,
				"upstream_status_code": record.UpstreamStatusCode,
			})
			if _, err := store.NewHealthRepository(tx).CreateSnapshot(ctx, store.CreateHealthSnapshotParams{
				SiteID:       record.SiteID,
				SiteModelID:  record.SiteModelID,
				Scope:        "model",
				Source:       "gateway",
				Endpoint:     stringValue(&record.Endpoint, gatewayEndpointChatCompletions),
				Method:       "POST",
				Success:      record.Success,
				StatusCode:   nullableInt(record.StatusCode),
				LatencyMS:    nullableInt64(record.UpstreamLatencyMS),
				ErrorType:    nullableString(record.ErrorType),
				ErrorMessage: nullableString(record.ErrorMessage),
				Metadata:     healthMetadata,
			}); err != nil {
				return err
			}
		}

		if err := store.NewRequestUsageSummaryRepository(tx).Increment(ctx, store.RequestUsageSummaryIncrement{
			RequestLog:  requestLog,
			UsageRecord: usageRecord,
			TimeZone:    r.timeZone,
		}); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return store.RequestLog{}, nil, err
	}

	if quotaOvershoot != nil && r.logger != nil {
		r.logger.WarnContext(ctx, "api key quota overshot soft limit",
			"scope", "gateway",
			"api_key_id", quotaOvershoot.ID,
			"quota_total_used", quotaOvershoot.EffectiveTotalQuotaUsed(),
			"quota_limit", quotaOvershoot.QuotaLimit.Float64,
			"request_id", record.RequestID,
		)
	}

	return requestLog, usageRecord, nil
}

type CompletionRecord = GatewayRequestRecord

func (r Recorder) RecordChatCompletion(ctx context.Context, record CompletionRecord) (store.RequestLog, *store.UsageRecord, error) {
	return r.RecordGatewayRequest(ctx, GatewayRequestRecord(record))
}

func recorderTimeZone(timeZones ...config.TimeZone) config.TimeZone {
	return config.TimeZoneOrDefault(timeZones...)
}

func nullableUUID(value uuid.UUID) any {
	if value == uuid.Nil {
		return nil
	}
	return value
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableInt(value int) any {
	if value <= 0 {
		return nil
	}
	return value
}

func nullableInt64(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}

func nullableFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}
