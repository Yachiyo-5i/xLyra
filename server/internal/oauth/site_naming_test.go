package oauth

import (
	"regexp"
	"testing"
)

func TestSiteSlugFromNameNormalizesSeparatorsAndDropsUnsupportedCharacters(t *testing.T) {
	t.Parallel()

	got := SiteSlugFromName("  Codex_Test  Prod!!  ")
	if got != "codex-test-prod" {
		t.Fatalf("SiteSlugFromName = %q, want codex-test-prod", got)
	}
}

func TestSiteSlugFromNameReturnsEmptyForUnsupportedCharactersOnly(t *testing.T) {
	t.Parallel()

	if got := SiteSlugFromName("你好!!!"); got != "" {
		t.Fatalf("SiteSlugFromName unsupported only = %q, want empty", got)
	}
}

func TestSiteNameFromEmailUsesDefaultPrefixAndEmailToken(t *testing.T) {
	t.Parallel()

	got, err := SiteNameFromEmail(" ", "User.Name@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^OAuth_user_[a-z]{4}$`).MatchString(got) {
		t.Fatalf("SiteNameFromEmail = %q, want OAuth_user_[a-z]{4}", got)
	}
}

func TestSiteNameFromEmailFallsBackToPrefixWithoutValidEmail(t *testing.T) {
	t.Parallel()

	got, err := SiteNameFromEmail(" Codex ", "not-an-email")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Codex" {
		t.Fatalf("SiteNameFromEmail = %q, want Codex", got)
	}
}

func TestCodexSiteNameFromEmailUsesCodexPrefix(t *testing.T) {
	t.Parallel()

	got, err := CodexSiteNameFromEmail("builder@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^Codex_buil_[a-z]{4}$`).MatchString(got) {
		t.Fatalf("CodexSiteNameFromEmail = %q, want Codex_buil_[a-z]{4}", got)
	}
}

func TestSiteNameFromEmailUsesClaudePrefix(t *testing.T) {
	t.Parallel()

	got, err := SiteNameFromEmail("Claude", "worker@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^Claude_work_[a-z]{4}$`).MatchString(got) {
		t.Fatalf("SiteNameFromEmail = %q, want Claude_work_[a-z]{4}", got)
	}
}

func TestImportedSiteNameUsesAntigravityPrefix(t *testing.T) {
	t.Parallel()

	got, err := ImportedSiteName(" ANTIGRAVITY ", "agent@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^Antigravity_agen_[a-z]{4}$`).MatchString(got) {
		t.Fatalf("ImportedSiteName = %q, want Antigravity_agen_[a-z]{4}", got)
	}
}

func TestImportedSiteNameDefaultsToCodexPrefix(t *testing.T) {
	t.Parallel()

	got, err := ImportedSiteName("codex", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Codex" {
		t.Fatalf("ImportedSiteName empty email = %q, want Codex", got)
	}
}
