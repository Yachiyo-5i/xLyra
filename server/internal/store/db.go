package store

import (
	"context"
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"xlyra/server/internal/config"
)

type Store struct {
	db *gorm.DB
}

func Open(ctx context.Context, cfg config.Config) (*Store, error) {
	db, err := gorm.Open(postgres.Open(cfg.DatabaseDSN()), &gorm.Config{
		TranslateError: true,
		Logger:         gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open postgres orm: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get postgres sql db: %w", err)
	}

	sqlDB.SetMaxIdleConns(int(cfg.DBMinConns))
	sqlDB.SetMaxOpenConns(int(cfg.DBMaxConns))
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)
	// Bound connection lifetime so pooled connections are recycled behind a
	// pgbouncer/load balancer instead of pinning to a single backend forever.
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, cfg.DBConnectTimeout)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() {
	if s == nil || s.db == nil {
		return
	}

	sqlDB, err := s.db.DB()
	if err == nil {
		_ = sqlDB.Close()
	}
}

func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store is not initialized")
	}

	sqlDB, err := s.db.DB()
	if err != nil {
		return fmt.Errorf("get postgres sql db: %w", err)
	}

	return sqlDB.PingContext(ctx)
}

func (s *Store) DB() *gorm.DB {
	if s == nil {
		return nil
	}

	return s.db
}
