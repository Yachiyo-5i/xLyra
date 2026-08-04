package site

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func TestAddedSiteIDsForGroupUpdateOnlyReturnsNewSites(t *testing.T) {
	t.Parallel()

	siteA := uuid.New()
	siteB := uuid.New()
	siteC := uuid.New()

	got := addedSiteIDsForGroupUpdate(true, []store.SiteGroupSite{
		{SiteID: siteA},
		{SiteID: siteB},
	}, []uuid.UUID{siteB, siteC, siteC})

	if len(got) != 1 || got[0] != siteC {
		t.Fatalf("expected only newly added site %s, got %v", siteC, got)
	}
}

func TestAddedSiteIDsForGroupUpdateTreatsReenabledGroupAsAdded(t *testing.T) {
	t.Parallel()

	siteA := uuid.New()
	siteB := uuid.New()

	got := addedSiteIDsForGroupUpdate(false, []store.SiteGroupSite{
		{SiteID: siteA, CreatedAt: time.Now()},
	}, []uuid.UUID{siteA, siteB})

	if len(got) != 2 || got[0] != siteA || got[1] != siteB {
		t.Fatalf("expected all current sites when re-enabling group, got %v", got)
	}
}

func TestFilterDeletedSitesWithUsageKeepsLoggedOrSummarizedDeletedSites(t *testing.T) {
	t.Parallel()

	activeID := uuid.New()
	loggedDeletedID := uuid.New()
	summarizedDeletedID := uuid.New()
	unusedDeletedID := uuid.New()

	got := filterDeletedSitesWithUsage([]store.Site{
		{ID: activeID, Status: "active"},
		{ID: loggedDeletedID, Status: store.SiteStatusDeleted},
		{ID: summarizedDeletedID, Status: store.SiteStatusDeleted},
		{ID: unusedDeletedID, Status: store.SiteStatusDeleted},
	}, map[uuid.UUID]bool{loggedDeletedID: true}, map[uuid.UUID]bool{summarizedDeletedID: true})

	if len(got) != 3 || got[0].ID != activeID || got[1].ID != loggedDeletedID || got[2].ID != summarizedDeletedID {
		t.Fatalf("expected active, logged deleted, and summarized deleted sites, got %#v", got)
	}
}

func TestSiteGroupParamsTrimsFieldsAndRequiresName(t *testing.T) {
	t.Parallel()

	params, err := siteGroupParams(SiteGroupInput{
		Name:        " Core Routes ",
		Slug:        " core-routes ",
		Description: " Primary production routes ",
		Enabled:     true,
		SortOrder:   20,
	})
	if err != nil {
		t.Fatalf("siteGroupParams returned error: %v", err)
	}
	if params.Name != "Core Routes" || params.Slug != "core-routes" || params.Description != "Primary production routes" {
		t.Fatalf("expected trimmed fields, got %#v", params)
	}
	if !params.Enabled || params.SortOrder != 20 {
		t.Fatalf("expected enabled/sort order to be preserved, got %#v", params)
	}

	if _, err := siteGroupParams(SiteGroupInput{Name: " \t\n "}); err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("blank name error = %v, want name is required", err)
	}
}

func TestValidateSiteIDsSkipsEmptyInputBeforeRepositoryAccess(t *testing.T) {
	t.Parallel()

	if err := validateSiteIDs(context.Background(), nil, nil); err != nil {
		t.Fatalf("empty site ids should not require repository access: %v", err)
	}
	if err := validateSiteIDs(context.Background(), nil, []uuid.UUID{uuid.Nil, uuid.Nil}); err != nil {
		t.Fatalf("nil-only site ids should not require repository access: %v", err)
	}
}
