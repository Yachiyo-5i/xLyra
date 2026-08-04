package cache

import "testing"

func TestAdminSessionKey(t *testing.T) {
	t.Parallel()

	if got := AdminSession("sess_123"); got != "xlyra:admin_session:sess_123" {
		t.Fatalf("unexpected admin session key %q", got)
	}
}

func TestHealthCooldownKey(t *testing.T) {
	t.Parallel()

	if got := HealthCooldown("site_1", "model_1"); got != "xlyra:health_cooldown:site_1:model_1" {
		t.Fatalf("unexpected health cooldown key %q", got)
	}
}

func TestCacheKeyBuilders(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		got  string
		want string
	}{
		{name: "api key", got: APIKey("sk_live"), want: "xlyra:api_key:sk_live"},
		{name: "site models", got: SiteModels("site_1"), want: "xlyra:site:site_1:models"},
		{name: "site balance", got: SiteBalance("site_1"), want: "xlyra:site:site_1:balance"},
		{name: "empty api key prefix", got: APIKey(""), want: "xlyra:api_key:"},
	}

	for _, tc := range cases {
		if tc.got != tc.want {
			t.Fatalf("%s key = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

func TestJoinKeepsEmptySegments(t *testing.T) {
	t.Parallel()

	if got := join("site", "", "models"); got != "xlyra:site::models" {
		t.Fatalf("join with empty segment = %q", got)
	}
	if got := join(); got != "xlyra" {
		t.Fatalf("join with no segments = %q", got)
	}
}
