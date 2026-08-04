package store

import (
	"context"
	"fmt"
	"net"
	"sort"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AdminAuditLog struct {
	ID             uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	ActorType      string
	AdminID        uuid.NullUUID
	AdminSessionID uuid.NullUUID
	AccessTokenID  uuid.NullUUID
	Action         string
	ResourceType   string
	ResourceID     string
	IPAddress      IPAddress `gorm:"column:ip_address"`
	UserAgent      string
	RequestID      string
	Success        bool
	ErrorCode      string
	Metadata       JSON `gorm:"type:jsonb"`
	CreatedAt      time.Time
}

type CreateAdminAuditLogParams struct {
	ActorType      string
	AdminID        any
	AdminSessionID any
	AccessTokenID  any
	Action         string
	ResourceType   string
	ResourceID     string
	IPAddress      net.IP
	UserAgent      string
	RequestID      string
	Success        bool
	ErrorCode      string
	Metadata       any
}

type AdminAuditLogFilters struct {
	Action    string
	ActorType string
	Success   *bool
	DateFrom  *time.Time
	DateTo    *time.Time
	Page      int
	PageSize  int
}

type AdminAuditLogRepository struct {
	db *gorm.DB
}

func NewAdminAuditLogRepository(db *gorm.DB) AdminAuditLogRepository {
	return AdminAuditLogRepository{db: db}
}

func (r AdminAuditLogRepository) Create(ctx context.Context, params CreateAdminAuditLogParams) (AdminAuditLog, error) {
	adminID := nullUUIDFromAny(params.AdminID)
	adminSessionID := nullUUIDFromAny(params.AdminSessionID)
	accessTokenID := nullUUIDFromAny(params.AccessTokenID)
	if adminID.Valid && !r.uuidExists(ctx, "admins", adminID.UUID) {
		adminID.Valid = false
	}
	if adminSessionID.Valid && !r.uuidExists(ctx, "admin_sessions", adminSessionID.UUID) {
		adminSessionID.Valid = false
	}
	if accessTokenID.Valid && !r.uuidExists(ctx, "admin_access_tokens", accessTokenID.UUID) {
		accessTokenID.Valid = false
	}

	item := AdminAuditLog{
		ActorType:      stringDefault(params.ActorType, "system"),
		AdminID:        adminID,
		AdminSessionID: adminSessionID,
		AccessTokenID:  accessTokenID,
		Action:         params.Action,
		ResourceType:   params.ResourceType,
		ResourceID:     params.ResourceID,
		IPAddress:      IPAddress(params.IPAddress),
		UserAgent:      params.UserAgent,
		RequestID:      params.RequestID,
		Success:        params.Success,
		ErrorCode:      params.ErrorCode,
		Metadata:       jsonFromAny(params.Metadata, "{}"),
	}
	if err := r.db.WithContext(ctx).Create(&item).Error; err != nil {
		return AdminAuditLog{}, fmt.Errorf("create admin audit log: %w", err)
	}
	return item, nil
}

func (r AdminAuditLogRepository) uuidExists(ctx context.Context, table string, id uuid.UUID) bool {
	var count int64
	db := r.db.WithContext(ctx)
	switch table {
	case "admins":
		db = db.Model(&Admin{}).Where(&Admin{ID: id})
	case "admin_sessions":
		db = db.Model(&AdminSession{}).Where(&AdminSession{ID: id})
	case "admin_access_tokens":
		db = db.Model(&AdminAccessToken{}).Where(&AdminAccessToken{ID: id})
	default:
		return true
	}
	if err := db.Count(&count).Error; err != nil {
		return true
	}
	return count > 0
}

func (r AdminAuditLogRepository) List(ctx context.Context, filters AdminAuditLogFilters) ([]AdminAuditLog, int64, error) {
	var all []AdminAuditLog
	if err := r.db.WithContext(ctx).Find(&all).Error; err != nil {
		return nil, 0, fmt.Errorf("list admin audit logs: %w", err)
	}
	filtered := all[:0]
	for _, item := range all {
		if !adminAuditLogMatches(item, filters) {
			continue
		}
		filtered = append(filtered, item)
	}
	total := int64(len(filtered))
	page := filters.Page
	if page < 1 {
		page = 1
	}
	pageSize := filters.PageSize
	if pageSize < 1 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})
	start := (page - 1) * pageSize
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + pageSize
	if end > len(filtered) {
		end = len(filtered)
	}
	items := filtered[start:end]
	return items, total, nil
}

func adminAuditLogMatches(item AdminAuditLog, filters AdminAuditLogFilters) bool {
	if filters.Action != "" && item.Action != filters.Action {
		return false
	}
	if filters.ActorType != "" && item.ActorType != filters.ActorType {
		return false
	}
	if filters.Success != nil && item.Success != *filters.Success {
		return false
	}
	if filters.DateFrom != nil && item.CreatedAt.Before(*filters.DateFrom) {
		return false
	}
	if filters.DateTo != nil && item.CreatedAt.After(*filters.DateTo) {
		return false
	}
	return true
}
