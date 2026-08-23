package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PlaygroundConversation struct {
	ID             uuid.UUID `gorm:"type:uuid;primaryKey"`
	AdminID        uuid.UUID `gorm:"type:uuid;not null;index:playground_conversations_admin_updated_idx,priority:1"`
	Mode           string    `gorm:"size:16;not null;index:playground_conversations_admin_updated_idx,priority:2"`
	Title          string
	RolloutPath    string `gorm:"not null"`
	LegacyClientID string `gorm:"size:128"`
	LastOrdinal    int64
	LastByteOffset int64
	CreatedAt      time.Time
	UpdatedAt      time.Time `gorm:"index:playground_conversations_admin_updated_idx,priority:3"`
}

func (PlaygroundConversation) TableName() string { return "playground_conversations" }

type PlaygroundRun struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey"`
	ConversationID  uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:playground_runs_conversation_idempotency_idx,priority:1;index:playground_runs_conversation_created_idx,priority:1"`
	APIKeyID        uuid.UUID `gorm:"type:uuid;not null"`
	IdempotencyKey  string    `gorm:"size:128;not null;uniqueIndex:playground_runs_conversation_idempotency_idx,priority:2"`
	Mode            string    `gorm:"size:16;not null"`
	Protocol        string    `gorm:"size:32"`
	Model           string
	Status          string `gorm:"size:24;not null;index"`
	Params          JSON   `gorm:"type:jsonb;not null"`
	CancelRequested bool
	Error           string
	StartedAt       *time.Time
	CompletedAt     *time.Time
	CreatedAt       time.Time `gorm:"index:playground_runs_conversation_created_idx,priority:2"`
	UpdatedAt       time.Time
}

func (PlaygroundRun) TableName() string { return "playground_runs" }

type PlaygroundTurnIndex struct {
	ID             uuid.UUID `gorm:"type:uuid;primaryKey"`
	ConversationID uuid.UUID `gorm:"type:uuid;not null;index"`
	RunID          uuid.UUID `gorm:"type:uuid;not null;uniqueIndex"`
	StartOrdinal   int64
	EndOrdinal     int64
	StartByte      int64
	EndByte        int64
	Status         string `gorm:"size:24;not null"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (PlaygroundTurnIndex) TableName() string { return "playground_turn_indexes" }

type PlaygroundAsset struct {
	ID             uuid.UUID     `gorm:"type:uuid;primaryKey"`
	ConversationID uuid.UUID     `gorm:"type:uuid;not null;index"`
	RunID          uuid.NullUUID `gorm:"type:uuid;index"`
	Purpose        string        `gorm:"size:32;not null"`
	Path           string        `gorm:"not null"`
	OriginalName   string
	MIMEType       string
	Size           int64
	SHA256         string `gorm:"size:64"`
	CreatedAt      time.Time
}

func (PlaygroundAsset) TableName() string { return "playground_assets" }

type PlaygroundRepository struct {
	db *gorm.DB
}

func NewPlaygroundRepository(db *gorm.DB) PlaygroundRepository {
	return PlaygroundRepository{db: db}
}

func (r PlaygroundRepository) CreateConversation(ctx context.Context, item *PlaygroundConversation) error {
	if err := r.db.WithContext(ctx).Create(item).Error; err != nil {
		return fmt.Errorf("create playground conversation: %w", err)
	}
	return nil
}

func (r PlaygroundRepository) GetConversation(ctx context.Context, adminID uuid.UUID, id uuid.UUID) (PlaygroundConversation, error) {
	var item PlaygroundConversation
	if err := r.db.WithContext(ctx).Where(&PlaygroundConversation{ID: id, AdminID: adminID}).First(&item).Error; err != nil {
		return PlaygroundConversation{}, err
	}
	return item, nil
}

func (r PlaygroundRepository) GetConversationByID(ctx context.Context, id uuid.UUID) (PlaygroundConversation, error) {
	var item PlaygroundConversation
	if err := r.db.WithContext(ctx).Where(&PlaygroundConversation{ID: id}).First(&item).Error; err != nil {
		return PlaygroundConversation{}, err
	}
	return item, nil
}

func (r PlaygroundRepository) ListConversations(ctx context.Context, adminID uuid.UUID, mode string, limit int) ([]PlaygroundConversation, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	query := r.db.WithContext(ctx).Where(&PlaygroundConversation{AdminID: adminID})
	if mode != "" {
		query = query.Where(&PlaygroundConversation{Mode: mode})
	}
	var items []PlaygroundConversation
	if err := query.Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "updated_at"}, Desc: true}}}).Limit(limit).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list playground conversations: %w", err)
	}
	return items, nil
}

func (r PlaygroundRepository) UpdateConversationCursor(ctx context.Context, id uuid.UUID, title string, ordinal int64, offset int64, updatedAt time.Time) error {
	updates := map[string]any{
		"title":            title,
		"last_ordinal":     ordinal,
		"last_byte_offset": offset,
		"updated_at":       updatedAt,
	}
	if err := r.db.WithContext(ctx).Model(&PlaygroundConversation{ID: id}).Updates(updates).Error; err != nil {
		return fmt.Errorf("update playground conversation cursor: %w", err)
	}
	return nil
}

func (r PlaygroundRepository) DeleteConversation(ctx context.Context, adminID uuid.UUID, id uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where(&PlaygroundAsset{ConversationID: id}).Delete(&PlaygroundAsset{}).Error; err != nil {
			return err
		}
		if err := tx.Where(&PlaygroundTurnIndex{ConversationID: id}).Delete(&PlaygroundTurnIndex{}).Error; err != nil {
			return err
		}
		if err := tx.Where(&PlaygroundRun{ConversationID: id}).Delete(&PlaygroundRun{}).Error; err != nil {
			return err
		}
		result := tx.Where(&PlaygroundConversation{ID: id, AdminID: adminID}).Delete(&PlaygroundConversation{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

func (r PlaygroundRepository) CreateRun(ctx context.Context, item *PlaygroundRun) (bool, error) {
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "conversation_id"}, {Name: "idempotency_key"}},
		DoNothing: true,
	}).Create(item)
	if result.Error != nil {
		return false, fmt.Errorf("create playground run: %w", result.Error)
	}
	return result.RowsAffected > 0, nil
}

func (r PlaygroundRepository) GetRunByIdempotency(ctx context.Context, conversationID uuid.UUID, key string) (PlaygroundRun, error) {
	var item PlaygroundRun
	if err := r.db.WithContext(ctx).Where(&PlaygroundRun{ConversationID: conversationID, IdempotencyKey: key}).First(&item).Error; err != nil {
		return PlaygroundRun{}, err
	}
	return item, nil
}

func (r PlaygroundRepository) GetRun(ctx context.Context, id uuid.UUID) (PlaygroundRun, error) {
	var item PlaygroundRun
	if err := r.db.WithContext(ctx).Where(&PlaygroundRun{ID: id}).First(&item).Error; err != nil {
		return PlaygroundRun{}, err
	}
	return item, nil
}

func (r PlaygroundRepository) LatestRun(ctx context.Context, conversationID uuid.UUID) (PlaygroundRun, error) {
	var item PlaygroundRun
	if err := r.db.WithContext(ctx).Where(&PlaygroundRun{ConversationID: conversationID}).Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "created_at"}, Desc: true}}}).First(&item).Error; err != nil {
		return PlaygroundRun{}, err
	}
	return item, nil
}

func (r PlaygroundRepository) UpdateRun(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	if err := r.db.WithContext(ctx).Model(&PlaygroundRun{ID: id}).Updates(updates).Error; err != nil {
		return fmt.Errorf("update playground run: %w", err)
	}
	return nil
}

func (r PlaygroundRepository) InterruptActiveRuns(ctx context.Context, now time.Time) error {
	statuses := []any{"queued", "running"}
	updates := map[string]any{"status": "interrupted", "error": "server restarted while the run was active", "completed_at": now, "updated_at": now}
	if err := r.db.WithContext(ctx).Model(&PlaygroundRun{}).Clauses(clause.Where{Exprs: []clause.Expression{clause.IN{Column: clause.Column{Name: "status"}, Values: statuses}}}).Updates(updates).Error; err != nil {
		return fmt.Errorf("interrupt active playground runs: %w", err)
	}
	return nil
}

func (r PlaygroundRepository) ListActiveRuns(ctx context.Context) ([]PlaygroundRun, error) {
	statuses := []any{"queued", "running"}
	var items []PlaygroundRun
	if err := r.db.WithContext(ctx).Clauses(clause.Where{Exprs: []clause.Expression{clause.IN{Column: clause.Column{Name: "status"}, Values: statuses}}}).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list active playground runs: %w", err)
	}
	return items, nil
}

func (r PlaygroundRepository) CreateTurnIndex(ctx context.Context, item *PlaygroundTurnIndex) error {
	if err := r.db.WithContext(ctx).Create(item).Error; err != nil {
		return fmt.Errorf("create playground turn index: %w", err)
	}
	return nil
}

func (r PlaygroundRepository) UpdateTurnIndex(ctx context.Context, runID uuid.UUID, status string, endOrdinal int64, endByte int64) error {
	updates := map[string]any{"status": status, "end_ordinal": endOrdinal, "end_byte": endByte, "updated_at": time.Now()}
	if err := r.db.WithContext(ctx).Model(&PlaygroundTurnIndex{}).Where(&PlaygroundTurnIndex{RunID: runID}).Updates(updates).Error; err != nil {
		return fmt.Errorf("update playground turn index: %w", err)
	}
	return nil
}

func (r PlaygroundRepository) CreateAsset(ctx context.Context, item *PlaygroundAsset) error {
	if err := r.db.WithContext(ctx).Create(item).Error; err != nil {
		return fmt.Errorf("create playground asset: %w", err)
	}
	return nil
}

func (r PlaygroundRepository) GetAsset(ctx context.Context, id uuid.UUID) (PlaygroundAsset, error) {
	var item PlaygroundAsset
	if err := r.db.WithContext(ctx).Where(&PlaygroundAsset{ID: id}).First(&item).Error; err != nil {
		return PlaygroundAsset{}, err
	}
	return item, nil
}

func (r PlaygroundRepository) DeleteAsset(ctx context.Context, id uuid.UUID) error {
	if err := r.db.WithContext(ctx).Where(&PlaygroundAsset{ID: id}).Delete(&PlaygroundAsset{}).Error; err != nil {
		return fmt.Errorf("delete playground asset: %w", err)
	}
	return nil
}
