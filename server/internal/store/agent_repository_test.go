package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestAgentTokenUsable(t *testing.T) {
	now := time.Now()
	activeRun := AgentRun{Status: AgentRunActive}
	pendingRun := AgentRun{Status: AgentRunPending}
	liveToken := AgentLLMToken{ExpiresAt: now.Add(10 * time.Minute)}

	tests := []struct {
		name  string
		token AgentLLMToken
		run   AgentRun
		want  bool
	}{
		{name: "live token on active run", token: liveToken, run: activeRun, want: true},
		{name: "live token on pending run", token: liveToken, run: pendingRun, want: false},
		{name: "live token on ended run", token: liveToken, run: AgentRun{Status: AgentRunEnded}, want: false},
		{name: "expired token", token: AgentLLMToken{ExpiresAt: now.Add(-time.Minute)}, run: activeRun, want: false},
		{
			name: "revoked token",
			token: func() AgentLLMToken {
				revoked := now.Add(-time.Minute)
				return AgentLLMToken{ExpiresAt: now.Add(10 * time.Minute), RevokedAt: &revoked}
			}(),
			run:  activeRun,
			want: false,
		},
		{
			name: "superseded within grace period",
			token: func() AgentLLMToken {
				superseded := now.Add(-SupersededGracePeriod / 2)
				return AgentLLMToken{ExpiresAt: now.Add(10 * time.Minute), SupersededAt: &superseded}
			}(),
			run:  activeRun,
			want: true,
		},
		{
			name: "superseded beyond grace period",
			token: func() AgentLLMToken {
				superseded := now.Add(-2 * SupersededGracePeriod)
				return AgentLLMToken{ExpiresAt: now.Add(10 * time.Minute), SupersededAt: &superseded}
			}(),
			run:  activeRun,
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AgentTokenUsable(tt.token, tt.run, now); got != tt.want {
				t.Fatalf("AgentTokenUsable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAgentTokenRenewable(t *testing.T) {
	now := time.Now()
	activeRun := AgentRun{Status: AgentRunActive}
	liveToken := AgentLLMToken{ExpiresAt: now.Add(10 * time.Minute)}

	tests := []struct {
		name  string
		token AgentLLMToken
		run   AgentRun
		want  bool
	}{
		{name: "live token on active run", token: liveToken, run: activeRun, want: true},
		{name: "expired within renewal grace", token: AgentLLMToken{ExpiresAt: now.Add(-RenewalGracePeriod / 2)}, run: activeRun, want: true},
		{name: "expired beyond renewal grace", token: AgentLLMToken{ExpiresAt: now.Add(-2 * RenewalGracePeriod)}, run: activeRun, want: false},
		{name: "live token on pending run", token: liveToken, run: AgentRun{Status: AgentRunPending}, want: false},
		{name: "live token on ended run", token: liveToken, run: AgentRun{Status: AgentRunEnded}, want: false},
		{
			name: "revoked token",
			token: func() AgentLLMToken {
				revoked := now.Add(-time.Minute)
				return AgentLLMToken{ExpiresAt: now.Add(10 * time.Minute), RevokedAt: &revoked}
			}(),
			run:  activeRun,
			want: false,
		},
		{
			name: "superseded token within grace is not renewable",
			token: func() AgentLLMToken {
				superseded := now.Add(-time.Second)
				return AgentLLMToken{ExpiresAt: now.Add(10 * time.Minute), SupersededAt: &superseded}
			}(),
			run:  activeRun,
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AgentTokenRenewable(tt.token, tt.run, now); got != tt.want {
				t.Fatalf("AgentTokenRenewable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAgentRunRegisterInsertsNewRun(t *testing.T) {
	db := storeTransactionGorm(t, "agent run register")
	created := storeCaptureCreate[AgentRun](t, db, "agent run", nil)
	queryCalls := 0
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		queryCalls++
		tx.Statement.RowsAffected = 1
	})

	repo := NewAgentRepository(db)
	input := AgentRunInput{AgentInstanceID: "inst-1", SessionID: "sess-1", RunID: "run-1", Model: "gpt-5"}
	run, err := repo.Register(context.Background(), input, time.Now())
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if created.SessionID != "sess-1" || created.RunID != "run-1" || created.Status != AgentRunActive {
		t.Fatalf("created run = %+v", *created)
	}
	if run.SessionID != "sess-1" || run.Status != AgentRunActive {
		t.Fatalf("Register() run = %+v", run)
	}
	if queryCalls != 0 {
		t.Fatalf("fresh insert must not re-read the row, got %d queries", queryCalls)
	}
}

func TestAgentRunRegisterConflictSameIdentityIsIdempotent(t *testing.T) {
	db := storeTransactionGorm(t, "agent run register conflict")
	// 并发注册时插入撞唯一索引（ON CONFLICT DO NOTHING 后 RowsAffected=0），
	// 回落为读取既有记录并校验身份——同身份视为重复注册，直接成功。
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		tx.Statement.RowsAffected = 0
	})
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*AgentRun)
		if !ok {
			tx.AddError(gorm.ErrRecordNotFound)
			return
		}
		*item = AgentRun{AgentInstanceID: "inst-1", SessionID: "sess-1", RunID: "run-1", Model: "gpt-5", Status: AgentRunActive}
		tx.Statement.RowsAffected = 1
	})

	repo := NewAgentRepository(db)
	input := AgentRunInput{AgentInstanceID: "inst-1", SessionID: "sess-1", RunID: "run-1", Model: "gpt-5"}
	run, err := repo.Register(context.Background(), input, time.Now())
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if run.Status != AgentRunActive {
		t.Fatalf("Register() run = %+v", run)
	}
}

func TestAgentRunRegisterConflictActivatesPendingRun(t *testing.T) {
	db := storeTransactionGorm(t, "agent run register pending")
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		tx.Statement.RowsAffected = 0
	})
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*AgentRun)
		if !ok {
			tx.AddError(gorm.ErrRecordNotFound)
			return
		}
		*item = AgentRun{ID: uuid.New(), AgentInstanceID: "inst-1", SessionID: "sess-1", RunID: "run-1", Model: "gpt-5", Status: AgentRunPending}
		tx.Statement.RowsAffected = 1
	})
	saved := storeCaptureUpdate[AgentRun](t, db, "agent run", nil)

	repo := NewAgentRepository(db)
	input := AgentRunInput{AgentInstanceID: "inst-1", SessionID: "sess-1", RunID: "run-1", Model: "gpt-5"}
	if _, err := repo.Register(context.Background(), input, time.Now()); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if saved.Status != AgentRunActive || saved.PendingExpiresAt != nil {
		t.Fatalf("saved run = %+v, want activated", *saved)
	}
}

func TestAgentRunRegisterConflictIdentityMismatch(t *testing.T) {
	db := storeTransactionGorm(t, "agent run register mismatch")
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		tx.Statement.RowsAffected = 0
	})
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*AgentRun)
		if !ok {
			tx.AddError(gorm.ErrRecordNotFound)
			return
		}
		*item = AgentRun{AgentInstanceID: "inst-1", SessionID: "sess-1", RunID: "run-1", Model: "other-model", Status: AgentRunActive}
		tx.Statement.RowsAffected = 1
	})

	repo := NewAgentRepository(db)
	input := AgentRunInput{AgentInstanceID: "inst-1", SessionID: "sess-1", RunID: "run-1", Model: "gpt-5"}
	_, err := repo.Register(context.Background(), input, time.Now())
	assertStoreRepositoryErrorContains(t, err, "identity conflict")
}
