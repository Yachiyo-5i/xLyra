package store

import (
	"testing"
	"time"
)

func TestAgentTokenUsable(t *testing.T) {
	now := time.Now()
	activeRun := AgentRun{Status: AgentRunActive}
	pendingRun := func(window time.Duration) AgentRun {
		expiresAt := now.Add(window)
		return AgentRun{Status: AgentRunPending, PendingExpiresAt: &expiresAt}
	}
	liveToken := AgentLLMToken{ExpiresAt: now.Add(10 * time.Minute)}

	tests := []struct {
		name  string
		token AgentLLMToken
		run   AgentRun
		want  bool
	}{
		{name: "live token on active run", token: liveToken, run: activeRun, want: true},
		{name: "live token on pending run within window", token: liveToken, run: pendingRun(time.Minute), want: true},
		{name: "live token on expired pending run", token: liveToken, run: pendingRun(-time.Minute), want: false},
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
