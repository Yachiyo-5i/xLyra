package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const CacheObservationRetention = 48 * time.Hour

type CacheObservation struct {
	ID                 uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	RequestLogID       uuid.UUID `gorm:"type:uuid;uniqueIndex:cache_observations_request_log_id_key"`
	APIKeyID           uuid.UUID `gorm:"type:uuid;index:cache_observations_lookup_idx,priority:1"`
	CanonicalModelID   uuid.UUID `gorm:"type:uuid;index:cache_observations_lookup_idx,priority:2"`
	Success            bool
	PrefixHash         string
	RootPrefixHash     string
	PrefixLineage      JSON `gorm:"type:jsonb"`
	LineageTruncated   bool
	SessionHash        string
	CacheDomainHash    string
	CacheFingerprint   string
	LineageDepth       int
	DownstreamProtocol string
	UpstreamProtocol   string
	CachePolicyHash    string
	ExpiresAt          time.Time `gorm:"index:cache_observations_expires_at_idx"`
	CreatedAt          time.Time `gorm:"index:cache_observations_lookup_idx,priority:3;index:cache_observations_created_at_idx"`
}

func (CacheObservation) TableName() string {
	return "cache_observations"
}

type CacheObservationPayload struct {
	PrefixHash         string    `json:"prefix_hash"`
	RootPrefixHash     string    `json:"root_prefix_hash"`
	PrefixLineage      []string  `json:"prefix_lineage"`
	LineageTruncated   bool      `json:"lineage_truncated"`
	SessionHash        string    `json:"session_hash"`
	CacheDomainHash    string    `json:"cache_domain_hash"`
	CacheFingerprint   string    `json:"cache_fingerprint"`
	LineageDepth       int       `json:"lineage_depth"`
	DownstreamProtocol string    `json:"downstream_protocol"`
	UpstreamProtocol   string    `json:"upstream_protocol"`
	CachePolicyHash    string    `json:"cache_policy_hash"`
	ExpiresAt          time.Time `json:"expires_at"`
}

type CacheObservationRepository struct {
	db *gorm.DB
}

func NewCacheObservationRepository(db *gorm.DB) CacheObservationRepository {
	return CacheObservationRepository{db: db}
}

func (r CacheObservationRepository) Create(ctx context.Context, requestLog RequestLog, payload CacheObservationPayload) error {
	if r.db == nil {
		return fmt.Errorf("cache observation store is not initialized")
	}
	lineage, _ := json.Marshal(payload.PrefixLineage)
	item := CacheObservation{
		RequestLogID:       requestLog.ID,
		APIKeyID:           requestLog.APIKeyID.UUID,
		CanonicalModelID:   requestLog.CanonicalModelID.UUID,
		Success:            requestLog.Success,
		PrefixHash:         payload.PrefixHash,
		RootPrefixHash:     payload.RootPrefixHash,
		PrefixLineage:      JSON(lineage),
		LineageTruncated:   payload.LineageTruncated,
		SessionHash:        payload.SessionHash,
		CacheDomainHash:    payload.CacheDomainHash,
		CacheFingerprint:   payload.CacheFingerprint,
		LineageDepth:       payload.LineageDepth,
		DownstreamProtocol: payload.DownstreamProtocol,
		UpstreamProtocol:   payload.UpstreamProtocol,
		CachePolicyHash:    payload.CachePolicyHash,
		ExpiresAt:          payload.ExpiresAt,
		CreatedAt:          requestLog.CreatedAt,
	}
	if !requestLog.Success || !requestLog.APIKeyID.Valid || !requestLog.CanonicalModelID.Valid || (item.PrefixHash == "" && item.SessionHash == "") {
		return nil
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&item).Error; err != nil {
		return fmt.Errorf("create cache observation: %w", err)
	}
	return nil
}

func (r CacheObservationRepository) ListRecent(ctx context.Context, apiKeyID uuid.UUID, canonicalModelID uuid.UUID, since time.Time, limit int) ([]RequestLogCacheObservation, error) {
	if r.db == nil {
		return nil, fmt.Errorf("cache observation store is not initialized")
	}
	var items []CacheObservation
	if err := r.db.WithContext(ctx).
		Clauses(clause.Where{Exprs: []clause.Expression{
			clause.Eq{Column: clause.Column{Name: "api_key_id"}, Value: apiKeyID},
			clause.Eq{Column: clause.Column{Name: "canonical_model_id"}, Value: canonicalModelID},
			clause.Gte{Column: clause.Column{Name: "created_at"}, Value: since},
			clause.Eq{Column: clause.Column{Name: "success"}, Value: true},
		}}).
		Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "created_at"}, Desc: true}}}).
		Limit(limit).
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list recent cache observations: %w", err)
	}
	result := make([]RequestLogCacheObservation, 0, len(items))
	for _, item := range items {
		result = append(result, RequestLogCacheObservation{
			RequestLogID:     item.RequestLogID,
			Success:          item.Success,
			PrefixHash:       item.PrefixHash,
			SessionHash:      item.SessionHash,
			CacheDomainHash:  item.CacheDomainHash,
			CacheFingerprint: item.CacheFingerprint,
			LineageDepth:     item.LineageDepth,
			ExpiresAt:        item.ExpiresAt,
			CreatedAt:        item.CreatedAt,
		})
	}
	return result, nil
}

func (r CacheObservationRepository) DeleteBefore(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	if r.db == nil {
		return 0, fmt.Errorf("cache observation store is not initialized")
	}
	if limit <= 0 {
		limit = 5000
	}
	var items []CacheObservation
	if err := r.db.WithContext(ctx).Model(&CacheObservation{}).
		Clauses(clause.Where{Exprs: []clause.Expression{clause.Lt{Column: clause.Column{Name: "created_at"}, Value: cutoff}}}).
		Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "created_at"}}}}).
		Limit(limit).Find(&items).Error; err != nil {
		return 0, fmt.Errorf("list expired cache observations: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	result := r.db.WithContext(ctx).Delete(&CacheObservation{}, ids)
	if result.Error != nil {
		return 0, fmt.Errorf("delete expired cache observations: %w", result.Error)
	}
	return result.RowsAffected, nil
}
