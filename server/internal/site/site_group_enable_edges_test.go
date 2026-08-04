package site

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

func TestSiteGroupCreateUpdateRejectBlankNameBeforeDatabase(t *testing.T) {
	t.Parallel()

	service := &Service{}
	ctx := context.Background()

	if group, err := service.CreateSiteGroup(ctx, SiteGroupInput{Name: " \t\n "}); group.ID != uuid.Nil || err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("CreateSiteGroup blank name = %#v, %v; want zero group and name guard", group, err)
	}
	if group, err := service.UpdateSiteGroup(ctx, uuid.New(), SiteGroupInput{Name: " \t\n "}); group.ID != uuid.Nil || err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("UpdateSiteGroup blank name = %#v, %v; want zero group and name guard", group, err)
	}
}

func TestSiteGroupEnableBlockedReasonStopsOnStatus(t *testing.T) {
	t.Parallel()

	service := &Service{}
	got, err := service.siteEnableBlockedReason(context.Background(), store.Site{
		ID:       uuid.New(),
		SiteType: "xlyra",
		Status:   " unavailable ",
	})
	if err != nil {
		t.Fatalf("siteEnableBlockedReason returned error: %v", err)
	}
	if got != "site status is unavailable" {
		t.Fatalf("siteEnableBlockedReason = %q, want status reason", got)
	}
}

func TestSiteGroupEnableBlockedReasonPropagatesStateError(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("site state repository stopped")
	service := siteServiceWithQueryError(t, queryErr)

	got, err := service.siteEnableBlockedReason(context.Background(), store.Site{
		ID:       uuid.New(),
		SiteType: "openai",
		Status:   "active",
	})
	if got != "" {
		t.Fatalf("siteEnableBlockedReason = %q, want empty reason", got)
	}
	assertSiteQueryError(t, "siteEnableBlockedReason", err, queryErr)
}

func TestSiteGroupEnableBlockedReasonAllowsMissingStateForNonOAuth(t *testing.T) {
	t.Parallel()

	service := siteServiceWithQueryCallback(t, func(tx *gorm.DB) {
		tx.AddError(gorm.ErrRecordNotFound)
	})

	got, err := service.siteEnableBlockedReason(context.Background(), store.Site{
		ID:       uuid.New(),
		SiteType: "openai",
		Status:   "active",
	})
	if err != nil || got != "" {
		t.Fatalf("siteEnableBlockedReason = %q, %v; want missing state allowed", got, err)
	}
}

func TestSiteGroupEnableBlockedReasonHandlesOAuthLookupResults(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	queryErr := errors.New("oauth repository stopped")
	service := siteServiceWithQueryCallback(t, func(tx *gorm.DB) {
		switch tx.Statement.Dest.(type) {
		case *store.SiteState:
			tx.AddError(gorm.ErrRecordNotFound)
		default:
			tx.AddError(queryErr)
		}
	})

	got, err := service.siteEnableBlockedReason(context.Background(), store.Site{
		ID:       siteID,
		SiteType: "codex",
		Status:   "active",
	})
	if got != "" {
		t.Fatalf("siteEnableBlockedReason = %q, want empty reason", got)
	}
	assertSiteQueryError(t, "siteEnableBlockedReason oauth", err, queryErr)

	missingOAuthService := siteServiceWithQueryCallback(t, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*store.SiteState); ok {
			tx.AddError(gorm.ErrRecordNotFound)
		}
	})
	got, err = missingOAuthService.siteEnableBlockedReason(context.Background(), store.Site{
		ID:       siteID,
		SiteType: "codex",
		Status:   "active",
	})
	if err != nil || got != "" {
		t.Fatalf("siteEnableBlockedReason missing oauth = %q, %v; want allowed", got, err)
	}
}
