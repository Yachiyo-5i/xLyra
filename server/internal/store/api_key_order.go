package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrAPIKeyOrderConflict = errors.New("api key order changed; reload before saving")
var ErrInvalidAPIKeyOrder = errors.New("api key order must contain every key exactly once")

func apiKeyOrderOption(key APIKey) APIKeyListOption {
	return APIKeyListOption{ID: key.ID, SortOrder: key.SortOrder, Status: key.Status, CreatedAt: key.CreatedAt}
}

func apiKeyOrderLess(a, b APIKeyListOption) bool {
	if a.SortOrder != b.SortOrder {
		if a.SortOrder == 0 || b.SortOrder == 0 {
			return a.SortOrder != 0
		}
		return a.SortOrder < b.SortOrder
	}
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.Before(b.CreatedAt)
	}
	return a.ID.String() < b.ID.String()
}

func SortAPIKeys(keys []APIKey) {
	sort.SliceStable(keys, func(i, j int) bool {
		return apiKeyOrderLess(apiKeyOrderOption(keys[i]), apiKeyOrderOption(keys[j]))
	})
}

func SortAPIKeyOptions(keys []APIKeyListOption) {
	sort.SliceStable(keys, func(i, j int) bool {
		return apiKeyOrderLess(keys[i], keys[j])
	})
}

func APIKeyOrderRevision(keys []APIKey) string {
	ordered := append([]APIKey(nil), keys...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID.String() < ordered[j].ID.String() })
	hash := sha256.New()
	for _, key := range ordered {
		fmt.Fprintf(hash, "%s:%d\n", key.ID, key.SortOrder)
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func (r APIKeyRepository) withOrderLock(ctx context.Context, fn func(*gorm.DB) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		marker := schemaUpgradeMarker{Name: "api_keys_display_order_v1", CompletedAt: time.Now()}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&marker).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).Where(&schemaUpgradeMarker{Name: marker.Name}).First(&marker).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}

func BackfillAPIKeyOrder(keys []APIKey) []APIKey {
	SortAPIKeys(keys)
	var last int64
	var changed []APIKey
	for i := range keys {
		if keys[i].SortOrder > last {
			last = keys[i].SortOrder
		}
		if keys[i].SortOrder != 0 {
			continue
		}
		last++
		keys[i].SortOrder = last
		changed = append(changed, keys[i])
	}
	return changed
}

func (r APIKeyRepository) InitializeOrder(ctx context.Context) error {
	return r.withOrderLock(ctx, func(tx *gorm.DB) error {
		keys, err := NewAPIKeyRepository(tx).List(ctx)
		if err != nil {
			return err
		}
		for _, key := range BackfillAPIKeyOrder(keys) {
			if err := tx.Model(&APIKey{ID: key.ID}).UpdateColumn("sort_order", key.SortOrder).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r APIKeyRepository) Reorder(ctx context.Context, ids []uuid.UUID, revision string) error {
	seen := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		if id == uuid.Nil || seen[id] {
			return ErrInvalidAPIKeyOrder
		}
		seen[id] = true
	}
	return r.withOrderLock(ctx, func(tx *gorm.DB) error {
		keys, err := NewAPIKeyRepository(tx).List(ctx)
		if err != nil {
			return err
		}
		if revision != APIKeyOrderRevision(keys) {
			return ErrAPIKeyOrderConflict
		}
		if len(ids) != len(keys) {
			return ErrInvalidAPIKeyOrder
		}
		for _, key := range keys {
			if !seen[key.ID] {
				return ErrInvalidAPIKeyOrder
			}
		}
		for index, id := range ids {
			if err := tx.Model(&APIKey{ID: id}).UpdateColumn("sort_order", int64(index+1)).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
