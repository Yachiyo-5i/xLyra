package router

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestCandidatesSiteTypeFilter(t *testing.T) {
	ctx := context.Background()
	canonicalID := uuid.New()
	rows := routerPlanRepositoryRows(canonicalID, "gpt-test", []string{"builtin", "plugin", "paused"})
	rows.sites[0].SiteType = "openai"
	rows.sites[1].SiteType = " ACME.EXAMPLE/Chat "
	rows.sites[2].SiteType = "paused.example/chat"
	service := routerServiceWithRepositoryRows(t, rows)

	all, err := service.Candidates(ctx, CandidateQuery{ModelKey: "gpt-test"})
	if err != nil {
		t.Fatal(err)
	}
	if got := candidateNames(all.Items); got != "builtin,paused,plugin" {
		t.Fatalf("nil filter candidates = %s", got)
	}

	service.SetSiteTypeFilter(allowEnabledPluginSiteType)
	filtered, err := service.Candidates(ctx, CandidateQuery{ModelKey: "gpt-test"})
	if err != nil {
		t.Fatal(err)
	}
	if got := candidateNames(filtered.Items); got != "builtin,plugin" {
		t.Fatalf("filtered candidates = %s", got)
	}
	for _, item := range filtered.Items {
		if strings.Contains(strings.ToLower(item.Site.SiteType), "paused") {
			t.Fatalf("unavailable site stayed in candidates: %#v", item.Site)
		}
	}

	service.SetSiteTypeFilter(nil)
	restored, err := service.Candidates(ctx, CandidateQuery{ModelKey: "gpt-test"})
	if err != nil {
		t.Fatal(err)
	}
	if got := candidateNames(restored.Items); got != "builtin,paused,plugin" {
		t.Fatalf("cleared filter candidates = %s", got)
	}
}

func TestSelectUnavailableSiteTypeUsesNoCandidateError(t *testing.T) {
	ctx := context.Background()
	canonicalID := uuid.New()
	rows := routerPlanRepositoryRows(canonicalID, "gpt-paused", []string{"paused"})
	rows.sites[0].SiteType = "paused.example/chat"
	service := routerServiceWithRepositoryRows(t, rows)
	service.SetSiteTypeFilter(allowEnabledPluginSiteType)

	_, err := service.Select(ctx, CandidateQuery{ModelKey: "gpt-paused"})
	if !errors.Is(err, ErrNoRouteCandidates) {
		t.Fatalf("Select error = %v, want ErrNoRouteCandidates", err)
	}

	service.SetSiteTypeFilter(nil)
	selected, err := service.Select(ctx, CandidateQuery{ModelKey: "gpt-paused"})
	if err != nil {
		t.Fatal(err)
	}
	if selected.Candidate.Site.Name != "paused" {
		t.Fatalf("nil filter selected %#v", selected.Candidate.Site)
	}
}

func allowEnabledPluginSiteType(siteType string) bool {
	key := strings.ToLower(strings.TrimSpace(siteType))
	if !strings.Contains(key, "/") {
		return true
	}
	return key == "acme.example/chat"
}

func candidateNames(items []Candidate) string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Site.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}
