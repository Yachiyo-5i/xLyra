package store

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

type Tx = *gorm.DB

func (s *Store) WithinTx(ctx context.Context, fn func(tx Tx) error) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store is not initialized")
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(tx)
	})
}
