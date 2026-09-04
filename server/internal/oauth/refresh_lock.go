package oauth

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
)

const refreshLockNamespace = "xlyra/oauth-refresh/"

// withPostgresAdvisoryLock holds a session-level advisory lock for the entire
// callback. The dedicated *sql.Conn keeps lock and unlock on one PostgreSQL session.
func withPostgresAdvisoryLock(ctx context.Context, db *sql.DB, key string, fn func(*sql.Conn) error) error {
	if db == nil {
		return fmt.Errorf("postgres refresh lock: database is nil")
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("postgres refresh lock: acquire connection: %w", err)
	}

	locked := false
	defer func() {
		if locked {
			_, _ = conn.ExecContext(context.Background(), "select pg_advisory_unlock(hashtextextended($1, 0))", key)
		}
		_ = conn.Close()
	}()

	if _, err := conn.ExecContext(ctx, "select pg_advisory_lock(hashtextextended($1, 0))", key); err != nil {
		return fmt.Errorf("postgres refresh lock: acquire advisory lock: %w", err)
	}
	locked = true
	return fn(conn)
}

func (s *Service) withRefreshConnectionLock(ctx context.Context, connectionID uuid.UUID, fn func() (CodexConnection, error)) (CodexConnection, error) {
	if s == nil || s.db == nil || s.db.DB() == nil || s.db.DB().Dialector.Name() != "postgres" {
		return fn()
	}

	db, err := s.db.DB().DB()
	if err != nil {
		return CodexConnection{}, fmt.Errorf("postgres refresh lock: get database: %w", err)
	}

	var result CodexConnection
	err = withPostgresAdvisoryLock(ctx, db, refreshLockNamespace+connectionID.String(), func(_ *sql.Conn) error {
		result, err = fn()
		return err
	})
	if err != nil {
		return CodexConnection{}, err
	}
	return result, nil
}
