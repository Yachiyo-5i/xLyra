package store

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

func upsertByKey[T any](ctx context.Context, db *gorm.DB, key T, label string, mutate func(item *T)) (T, error) {
	var item T
	err := db.WithContext(ctx).Where(&key).First(&item).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return item, fmt.Errorf("%s: %w", label, err)
	}
	mutate(&item)
	if err == gorm.ErrRecordNotFound {
		if createErr := db.WithContext(ctx).Create(&item).Error; createErr != nil {
			return item, fmt.Errorf("%s: %w", label, createErr)
		}
		return item, nil
	}
	if saveErr := db.WithContext(ctx).Save(&item).Error; saveErr != nil {
		return item, fmt.Errorf("%s: %w", label, saveErr)
	}
	return item, nil
}
