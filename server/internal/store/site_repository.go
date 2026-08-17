package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Site struct {
	ID              uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	Name            string
	Slug            string
	SiteType        string
	BaseURL         string
	Status          string
	Enabled         bool
	RoutingPriority float64
	Meta            JSON `gorm:"type:jsonb"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

const SiteStatusDeleted = "deleted"

type CreateSiteParams struct {
	Name            string
	Slug            string
	SiteType        string
	BaseURL         string
	Status          string
	Enabled         bool
	RoutingPriority float64
	Meta            JSON
}

type UpdateSiteParams struct {
	ID              uuid.UUID
	Name            string
	Slug            string
	SiteType        string
	BaseURL         string
	Status          string
	Enabled         bool
	RoutingPriority float64
	Meta            JSON
}

type SiteRepository struct {
	db *gorm.DB
}

type SiteListOption struct {
	ID              uuid.UUID
	Name            string
	Status          string
	Enabled         bool
	RoutingPriority float64
	CreatedAt       time.Time
}

func (SiteListOption) TableName() string {
	return "sites"
}

func NewSiteRepository(db *gorm.DB) SiteRepository {
	return SiteRepository{db: db}
}

func SiteDeleted(site Site) bool {
	return site.Status == SiteStatusDeleted
}

func (r SiteRepository) Create(ctx context.Context, params CreateSiteParams) (Site, error) {
	site := Site{
		Name:            params.Name,
		Slug:            params.Slug,
		SiteType:        params.SiteType,
		BaseURL:         params.BaseURL,
		Status:          params.Status,
		Enabled:         params.Enabled,
		RoutingPriority: params.RoutingPriority,
		Meta:            jsonDefault(params.Meta, "{}"),
	}
	if err := r.db.WithContext(ctx).Create(&site).Error; err != nil {
		return Site{}, fmt.Errorf("create site: %w", err)
	}
	return site, nil
}

func (r SiteRepository) Update(ctx context.Context, params UpdateSiteParams) (Site, error) {
	site, err := r.GetByID(ctx, params.ID)
	if err != nil {
		return Site{}, err
	}
	site.Name = params.Name
	site.Slug = params.Slug
	site.SiteType = params.SiteType
	site.BaseURL = params.BaseURL
	site.Status = params.Status
	site.Enabled = params.Enabled
	site.RoutingPriority = params.RoutingPriority
	site.Meta = jsonDefault(params.Meta, "{}")
	if err := r.db.WithContext(ctx).Save(&site).Error; err != nil {
		return Site{}, fmt.Errorf("update site: %w", err)
	}
	return site, nil
}

func (r SiteRepository) SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) (Site, error) {
	site, err := r.GetByID(ctx, id)
	if err != nil {
		return Site{}, err
	}
	site.Enabled = enabled
	if err := r.db.WithContext(ctx).Save(&site).Error; err != nil {
		return Site{}, fmt.Errorf("set site enabled: %w", err)
	}
	return site, nil
}

func (r SiteRepository) SetEnabledAndMeta(ctx context.Context, id uuid.UUID, enabled bool, meta JSON) (Site, error) {
	site, err := r.GetByID(ctx, id)
	if err != nil {
		return Site{}, err
	}
	site.Enabled = enabled
	site.Meta = jsonDefault(meta, "{}")
	if err := r.db.WithContext(ctx).Save(&site).Error; err != nil {
		return Site{}, fmt.Errorf("set site enabled and meta: %w", err)
	}
	return site, nil
}

func (r SiteRepository) UpdateMeta(ctx context.Context, id uuid.UUID, meta JSON) (Site, error) {
	site, err := r.GetByID(ctx, id)
	if err != nil {
		return Site{}, err
	}
	site.Meta = jsonDefault(meta, "{}")
	if err := r.db.WithContext(ctx).Save(&site).Error; err != nil {
		return Site{}, fmt.Errorf("update site meta: %w", err)
	}
	return site, nil
}

func (r SiteRepository) Delete(ctx context.Context, id uuid.UUID) error {
	site, err := r.GetByIDIncludingDeleted(ctx, id)
	if err != nil {
		return err
	}
	if SiteDeleted(site) {
		return fmt.Errorf("delete site: not found")
	}
	originalSlug := site.Slug
	site.Enabled = false
	site.Status = SiteStatusDeleted
	site.Slug = deletedSiteSlug(site.Slug, site.ID)
	site.Meta = deletedSiteMeta(site.Meta, originalSlug)
	if err := r.db.WithContext(ctx).Save(&site).Error; err != nil {
		return fmt.Errorf("delete site: %w", err)
	}
	return nil
}

func (r SiteRepository) List(ctx context.Context) ([]Site, error) {
	sites, err := r.ListIncludingDeleted(ctx)
	if err != nil {
		return nil, err
	}
	return filterActiveSites(sites), nil
}

func (r SiteRepository) ListOptions(ctx context.Context) ([]SiteListOption, error) {
	var items []SiteListOption
	if err := r.db.WithContext(ctx).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list site options: %w", err)
	}
	result := make([]SiteListOption, 0, len(items))
	for _, item := range items {
		if item.Status != SiteStatusDeleted {
			result = append(result, item)
		}
	}
	return result, nil
}

func (r SiteRepository) ListIncludingDeleted(ctx context.Context) ([]Site, error) {
	var sites []Site
	if err := r.db.WithContext(ctx).Find(&sites).Error; err != nil {
		return nil, fmt.Errorf("list sites: %w", err)
	}
	sort.SliceStable(sites, func(i, j int) bool {
		return sites[i].CreatedAt.After(sites[j].CreatedAt)
	})
	return sites, nil
}

func (r SiteRepository) ListByIDsIncludingDeleted(ctx context.Context, ids []uuid.UUID) ([]Site, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var sites []Site
	if err := r.db.WithContext(ctx).Where(map[string]any{"id": ids}).Find(&sites).Error; err != nil {
		return nil, fmt.Errorf("list sites by ids: %w", err)
	}
	return sites, nil
}

func (r SiteRepository) GetByID(ctx context.Context, id uuid.UUID) (Site, error) {
	site, err := r.GetByIDIncludingDeleted(ctx, id)
	if err != nil {
		return Site{}, err
	}
	if SiteDeleted(site) {
		return Site{}, fmt.Errorf("get site: %w", gorm.ErrRecordNotFound)
	}
	return site, nil
}

func (r SiteRepository) GetByIDIncludingDeleted(ctx context.Context, id uuid.UUID) (Site, error) {
	var site Site
	if err := r.db.WithContext(ctx).Where(&Site{ID: id}).First(&site).Error; err != nil {
		return Site{}, fmt.Errorf("get site: %w", err)
	}
	return site, nil
}

func (r SiteRepository) FindBySlug(ctx context.Context, slug string) (Site, error) {
	var site Site
	if err := r.db.WithContext(ctx).Where(&Site{Slug: slug}).First(&site).Error; err != nil {
		return Site{}, fmt.Errorf("find site by slug: %w", err)
	}
	if SiteDeleted(site) {
		return Site{}, fmt.Errorf("find site by slug: %w", gorm.ErrRecordNotFound)
	}
	return site, nil
}

func (r SiteRepository) FindOAuthSite(ctx context.Context, siteType string, accountID string, email string) (Site, error) {
	var sites []Site
	if err := r.db.WithContext(ctx).Where(&Site{SiteType: siteType}).Find(&sites).Error; err != nil {
		return Site{}, fmt.Errorf("find oauth site: %w", err)
	}
	sites = filterActiveSites(sites)
	sort.SliceStable(sites, func(i, j int) bool {
		if !sites[i].CreatedAt.Equal(sites[j].CreatedAt) {
			return sites[i].CreatedAt.Before(sites[j].CreatedAt)
		}
		return sites[i].ID.String() < sites[j].ID.String()
	})
	if site, ok := findOAuthSiteByIdentity(sites, accountID, email); ok {
		return site, nil
	}
	return Site{}, fmt.Errorf("find oauth site: %w", gorm.ErrRecordNotFound)
}

func findOAuthSiteByIdentity(sites []Site, accountID string, email string) (Site, bool) {
	email = strings.TrimSpace(email)
	if email != "" {
		for _, site := range sites {
			if strings.EqualFold(siteMetaString(site.Meta, "oauth_email"), email) {
				return site, true
			}
		}
	}

	accountID = strings.TrimSpace(accountID)
	if accountID != "" && email == "" {
		for _, site := range sites {
			if siteMetaString(site.Meta, "oauth_account_id") == accountID {
				return site, true
			}
		}
	}
	return Site{}, false
}

func filterActiveSites(items []Site) []Site {
	filtered := items[:0]
	for _, item := range items {
		if SiteDeleted(item) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func deletedSiteSlug(slug string, id uuid.UUID) string {
	if strings.TrimSpace(slug) == "" {
		slug = "site"
	}
	idText := strings.ReplaceAll(id.String(), "-", "")
	if len(idText) > 8 {
		idText = idText[:8]
	}
	return strings.TrimSpace(slug) + "-deleted-" + idText
}

func deletedSiteMeta(raw JSON, originalSlug string) JSON {
	meta := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &meta)
	}
	if _, ok := meta["deleted_at"]; !ok {
		meta["deleted_at"] = time.Now().UTC().Format(time.RFC3339)
	}
	if _, ok := meta["deleted_original_slug"]; !ok {
		meta["deleted_original_slug"] = originalSlug
	}
	encoded, err := json.Marshal(meta)
	if err != nil {
		return jsonDefault(raw, "{}")
	}
	return JSON(encoded)
}

func siteMetaString(meta JSON, key string) string {
	if len(meta) == 0 {
		return ""
	}
	var values map[string]any
	if err := json.Unmarshal(meta, &values); err != nil {
		return ""
	}
	value, ok := values[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return text
}

func IsRecordNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}
