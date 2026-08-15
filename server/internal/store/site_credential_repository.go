package store

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type SiteCredential struct {
	ID                     uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	SiteID                 uuid.UUID
	CredentialType         string
	EncryptedSecret        string
	MaskedSecret           string
	DisplayName            string  `gorm:"not null;default:''"`
	RoutingPriority        float64 `gorm:"type:numeric(2,1);not null;default:1.0"`
	UpstreamCostMultiplier float64 `gorm:"type:numeric(8,4);not null;default:1.0"`
	Meta                   JSON    `gorm:"type:jsonb"`
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type UpdateSiteCredentialRoutingConfigParams struct {
	DisplayName            *string
	RoutingPriority        *float64
	UpstreamCostMultiplier *float64
}

type UpsertSiteCredentialParams struct {
	SiteID          uuid.UUID
	CredentialType  string
	EncryptedSecret string
	MaskedSecret    string
	Meta            JSON
}

type SiteCredentialRepository struct {
	db *gorm.DB
}

func SiteCredentialCacheDomain(credential SiteCredential) string {
	var metadata map[string]any
	if len(credential.Meta) == 0 || json.Unmarshal(credential.Meta, &metadata) != nil {
		return ""
	}
	value, _ := metadata["cache_domain"].(string)
	return strings.TrimSpace(value)
}

func NewSiteCredentialRepository(db *gorm.DB) SiteCredentialRepository {
	return SiteCredentialRepository{db: db}
}

func (r SiteCredentialRepository) Upsert(ctx context.Context, params UpsertSiteCredentialParams) (SiteCredential, error) {
	return upsertByKey(ctx, r.db, SiteCredential{SiteID: params.SiteID, CredentialType: params.CredentialType}, "upsert site credential", func(credential *SiteCredential) {
		credential.SiteID = params.SiteID
		credential.CredentialType = params.CredentialType
		credential.EncryptedSecret = params.EncryptedSecret
		credential.MaskedSecret = params.MaskedSecret
		credential.Meta = jsonDefault(params.Meta, "{}")
	})
}

func (r SiteCredentialRepository) GetBySiteAndType(ctx context.Context, siteID uuid.UUID, credentialType string) (SiteCredential, error) {
	var credential SiteCredential
	if err := r.db.WithContext(ctx).Where(&SiteCredential{SiteID: siteID, CredentialType: credentialType}).First(&credential).Error; err != nil {
		return SiteCredential{}, fmt.Errorf("get site credential: %w", err)
	}
	return credential, nil
}

func (r SiteCredentialRepository) GetByID(ctx context.Context, id uuid.UUID) (SiteCredential, error) {
	var credential SiteCredential
	if err := r.db.WithContext(ctx).Where(&SiteCredential{ID: id}).First(&credential).Error; err != nil {
		return SiteCredential{}, fmt.Errorf("get site credential by id: %w", err)
	}
	return credential, nil
}

func (r SiteCredentialRepository) ListBySite(ctx context.Context, siteID uuid.UUID) ([]SiteCredential, error) {
	var credentials []SiteCredential
	if err := r.db.WithContext(ctx).Where(&SiteCredential{SiteID: siteID}).Find(&credentials).Error; err != nil {
		return nil, fmt.Errorf("list site credentials: %w", err)
	}
	sort.SliceStable(credentials, func(i, j int) bool {
		leftAPIKey := isAPIKeyCredentialType(credentials[i].CredentialType)
		rightAPIKey := isAPIKeyCredentialType(credentials[j].CredentialType)
		if leftAPIKey && rightAPIKey {
			if !credentials[i].CreatedAt.Equal(credentials[j].CreatedAt) {
				return credentials[i].CreatedAt.Before(credentials[j].CreatedAt)
			}
			return credentials[i].ID.String() < credentials[j].ID.String()
		}
		return credentials[i].CredentialType < credentials[j].CredentialType
	})
	return credentials, nil
}

func (r SiteCredentialRepository) ListAll(ctx context.Context) ([]SiteCredential, error) {
	var credentials []SiteCredential
	if err := r.db.WithContext(ctx).Find(&credentials).Error; err != nil {
		return nil, fmt.Errorf("list site credentials: %w", err)
	}
	sort.SliceStable(credentials, func(i, j int) bool {
		if credentials[i].SiteID != credentials[j].SiteID {
			return credentials[i].SiteID.String() < credentials[j].SiteID.String()
		}
		return credentials[i].CredentialType < credentials[j].CredentialType
	})
	return credentials, nil
}

func (r SiteCredentialRepository) DeleteAPIKeysBySite(ctx context.Context, siteID uuid.UUID) error {
	credentials, err := r.ListBySite(ctx, siteID)
	if err != nil {
		return err
	}
	ids := make([]uuid.UUID, 0)
	for _, credential := range credentials {
		if credential.CredentialType == "api_key" || strings.HasPrefix(credential.CredentialType, "api_key:") {
			ids = append(ids, credential.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	if err := r.db.WithContext(ctx).Delete(&SiteCredential{}, ids).Error; err != nil {
		return fmt.Errorf("delete site api keys: %w", err)
	}
	return nil
}

func (r SiteCredentialRepository) DeleteBySite(ctx context.Context, siteID uuid.UUID) error {
	if err := r.db.WithContext(ctx).Where(&SiteCredential{SiteID: siteID}).Delete(&SiteCredential{}).Error; err != nil {
		return fmt.Errorf("delete site credentials: %w", err)
	}
	return nil
}

func (r SiteCredentialRepository) DeleteBySiteAndType(ctx context.Context, siteID uuid.UUID, credentialType string) error {
	credentials, err := r.ListBySite(ctx, siteID)
	if err != nil {
		return err
	}
	ids := make([]uuid.UUID, 0)
	for _, credential := range credentials {
		if credential.CredentialType == credentialType {
			ids = append(ids, credential.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	if err := r.db.WithContext(ctx).Delete(&SiteCredential{}, ids).Error; err != nil {
		return fmt.Errorf("delete site credential by type: %w", err)
	}
	return nil
}

func (r SiteCredentialRepository) DeleteByID(ctx context.Context, id uuid.UUID) error {
	if err := r.db.WithContext(ctx).Delete(&SiteCredential{}, id).Error; err != nil {
		return fmt.Errorf("delete site credential by id: %w", err)
	}
	return nil
}

func (r SiteCredentialRepository) UpdateMeta(ctx context.Context, id uuid.UUID, meta JSON) (SiteCredential, error) {
	credential, err := r.GetByID(ctx, id)
	if err != nil {
		return SiteCredential{}, err
	}
	credential.Meta = jsonDefault(meta, "{}")
	if err := r.db.WithContext(ctx).Save(&credential).Error; err != nil {
		return SiteCredential{}, fmt.Errorf("update site credential meta: %w", err)
	}
	return credential, nil
}

func (r SiteCredentialRepository) UpdateRoutingConfig(ctx context.Context, id uuid.UUID, params UpdateSiteCredentialRoutingConfigParams) (SiteCredential, error) {
	updates := map[string]any{}
	if params.DisplayName != nil {
		updates["display_name"] = *params.DisplayName
	}
	if params.RoutingPriority != nil {
		updates["routing_priority"] = *params.RoutingPriority
	}
	if params.UpstreamCostMultiplier != nil {
		updates["upstream_cost_multiplier"] = *params.UpstreamCostMultiplier
	}
	if len(updates) > 0 {
		result := r.db.WithContext(ctx).Model(&SiteCredential{ID: id}).Updates(updates)
		if result.Error != nil {
			return SiteCredential{}, fmt.Errorf("update site credential routing config: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return SiteCredential{}, fmt.Errorf("update site credential routing config: %w", gorm.ErrRecordNotFound)
		}
	}
	return r.GetByID(ctx, id)
}

func (r SiteCredentialRepository) UpdateSecret(ctx context.Context, id uuid.UUID, encryptedSecret string) (SiteCredential, error) {
	var credential SiteCredential
	db := r.db.WithContext(ctx)
	if err := db.Where(&SiteCredential{ID: id}).First(&credential).Error; err != nil {
		return SiteCredential{}, fmt.Errorf("update site credential secret: %w", err)
	}
	credential.EncryptedSecret = encryptedSecret
	if err := db.Save(&credential).Error; err != nil {
		return SiteCredential{}, fmt.Errorf("update site credential secret: %w", err)
	}
	return credential, nil
}

func (r SiteCredentialRepository) UpdateCredentialType(ctx context.Context, id uuid.UUID, credentialType string) (SiteCredential, error) {
	credential, err := r.GetByID(ctx, id)
	if err != nil {
		return SiteCredential{}, err
	}
	credential.CredentialType = strings.TrimSpace(credentialType)
	if err := r.db.WithContext(ctx).Save(&credential).Error; err != nil {
		return SiteCredential{}, fmt.Errorf("update site credential type: %w", err)
	}
	return credential, nil
}
