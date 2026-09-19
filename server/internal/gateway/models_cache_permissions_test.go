package gateway

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/store"
)

func TestModelsPayloadRechecksPermissionsAcrossInstances(t *testing.T) {
	key := store.APIKey{ID: uuid.New(), SitePolicy: "allow_list", ModelPolicy: "allow_all"}
	oldAccess := auth.APIKeyAccessSets{APIKey: key, AllowedSiteIDs: []uuid.UUID{uuid.New()}}
	lookups := 0
	db := gatewayGormWithQueryCallback(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.APIKeySitePermission:
			lookups++
			*dest = nil
		case *[]store.APIKeySiteGroupPermission:
			*dest = nil
		default:
			t.Fatalf("unexpected query destination %T", dest)
		}
	})
	for range 2 {
		handler := Handler{auth: auth.NewService(db, "test-master-key"), modelsCache: newModelsCache()}
		handler.modelsCache.items[key.ID] = modelsCacheEntry{
			payload: map[string]any{"data": []map[string]any{{"id": "removed-model"}}},
			cached:  time.Now(), revision: modelsAccessRevision(oldAccess),
		}
		payload, err := handler.modelsPayloadForAPIKey(context.Background(), key)
		if err != nil {
			t.Fatal(err)
		}
		if rows, ok := payload["data"].([]map[string]any); !ok || len(rows) != 0 {
			t.Fatalf("instance served removed models: %v", payload)
		}
	}
	if lookups != 2 {
		t.Fatalf("permission lookups = %d, want one per instance", lookups)
	}
}

func TestModelsCacheNewRequestsDoNotJoinInvalidatedBuild(t *testing.T) {
	for _, mode := range []string{"key", "all", "revision"} {
		t.Run(mode, func(t *testing.T) {
			cache := newModelsCache()
			key := store.APIKey{ID: uuid.New()}
			oldStarted, oldRelease, oldDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
			newStarted, newRelease, newDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			go func() {
				defer close(oldDone)
				_, _ = cache.getOrBuild(ctx, key, "before", func(context.Context, store.APIKey) (map[string]any, error) {
					close(oldStarted)
					select {
					case <-oldRelease:
					case <-ctx.Done():
					}
					return map[string]any{"object": "old"}, nil
				})
			}()
			<-oldStarted
			revision := "before"
			switch mode {
			case "key":
				cache.invalidateKey(key.ID)
			case "all":
				cache.invalidate()
			case "revision":
				revision = "after"
			}
			go func() {
				defer close(newDone)
				_, _ = cache.getOrBuild(ctx, key, revision, func(context.Context, store.APIKey) (map[string]any, error) {
					close(newStarted)
					select {
					case <-newRelease:
					case <-ctx.Done():
					}
					return map[string]any{"object": "new"}, nil
				})
			}()
			select {
			case <-newStarted:
			case <-ctx.Done():
				t.Fatal("new request joined the invalidated build")
			}
			cache.mu.Lock()
			newCall := cache.inflight[key.ID]
			cache.mu.Unlock()
			close(oldRelease)
			<-oldDone
			cache.mu.Lock()
			retained := cache.inflight[key.ID] == newCall
			_, staleStored := cache.items[key.ID]
			cache.mu.Unlock()
			if !retained || staleStored {
				t.Fatal("old completion must not remove the new build or store its old result")
			}
			close(newRelease)
			<-newDone
			payload, ok := cache.get(key.ID)
			if !ok || payload["object"] != "new" {
				t.Fatalf("cache = %v, want new permissions", payload)
			}
		})
	}
}

func TestModelsCacheRevisionChangeBypassesFreshAndStaleEntries(t *testing.T) {
	for _, age := range []time.Duration{0, modelsCacheFreshTTL + time.Second} {
		cache := newModelsCache()
		key := store.APIKey{ID: uuid.New()}
		cache.items[key.ID] = modelsCacheEntry{payload: map[string]any{"object": "old"}, cached: time.Now().Add(-age), revision: "old"}
		payload, err := cache.getOrBuild(context.Background(), key, "new", func(context.Context, store.APIKey) (map[string]any, error) {
			return map[string]any{"object": "new"}, nil
		})
		if err != nil || payload["object"] != "new" {
			t.Fatalf("age=%s payload=%v error=%v", age, payload, err)
		}
	}
}

func TestModelsAccessRevisionTracksOnlyEffectivePermissions(t *testing.T) {
	site, model := uuid.New(), uuid.New()
	base := auth.APIKeyAccessSets{
		APIKey:         store.APIKey{SitePolicy: "allow_list", ModelPolicy: "allow_list", ModelMappings: store.JSON("[]")},
		AllowedSiteIDs: []uuid.UUID{site}, AllowedSiteModelIDs: []uuid.UUID{model},
	}
	revision := modelsAccessRevision(base)
	for _, change := range []func(*auth.APIKeyAccessSets){
		func(a *auth.APIKeyAccessSets) { a.AllowedSiteIDs = []uuid.UUID{uuid.New()} },
		func(a *auth.APIKeyAccessSets) { a.AllowedSiteModelIDs = nil },
		func(a *auth.APIKeyAccessSets) { a.APIKey.SitePolicy = "allow_all" },
		func(a *auth.APIKeyAccessSets) { a.APIKey.ModelPolicy = "allow_all" },
		func(a *auth.APIKeyAccessSets) {
			a.APIKey.ModelMappings = store.JSON(`[{"pattern":"alias","target":"model"}]`)
		},
	} {
		changed := base
		change(&changed)
		if modelsAccessRevision(changed) == revision {
			t.Fatal("permission change did not change the revision")
		}
	}
	base.APIKey.UpdatedAt = time.Now()
	base.APIKey.LastUsedAt = &base.APIKey.UpdatedAt
	base.APIKey.QuotaUsed = 5
	base.APIKey.SortOrder = 3
	base.APIKey.Name = "renamed"
	if modelsAccessRevision(base) != revision {
		t.Fatal("unrelated changes must not invalidate models")
	}
	base.AllowedSiteIDs = []uuid.UUID{site, model}
	base.AllowedSiteModelIDs = []uuid.UUID{model, site}
	revision = modelsAccessRevision(base)
	base.AllowedSiteIDs = []uuid.UUID{model, site}
	base.AllowedSiteModelIDs = []uuid.UUID{site, model}
	if modelsAccessRevision(base) != revision {
		t.Fatal("database row ordering must not change the revision")
	}
}

func TestModelsCacheCanceledCallerDoesNotCancelSharedBuild(t *testing.T) {
	cache := newModelsCache()
	key := store.APIKey{ID: uuid.New()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan context.Context, 1)
	release := make(chan struct{})
	defer close(release)
	var builds atomic.Int32
	build := func(ctx context.Context, _ store.APIKey) (map[string]any, error) {
		builds.Add(1)
		started <- ctx
		select {
		case <-release:
			return map[string]any{"object": "fresh"}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	first := make(chan error, 1)
	go func() {
		_, err := cache.getOrBuild(ctx, key, "permissions", build)
		first <- err
	}()
	buildCtx := <-started
	cancel()
	select {
	case err := <-first:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled caller error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled caller remained blocked on the shared build")
	}
	if buildCtx.Err() != nil {
		t.Fatal("caller cancellation reached the shared build")
	}
	if deadline, ok := buildCtx.Deadline(); !ok || time.Until(deadline) > modelsCacheRefreshTimeout {
		t.Fatal("shared build must have a bounded lifetime")
	}
	waitingCtx, stopWaiting := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer stopWaiting()
	if _, err := cache.getOrBuild(waitingCtx, key, "permissions", build); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting caller error = %v", err)
	}
	if buildCtx.Err() != nil || builds.Load() != 1 {
		t.Fatal("canceling a waiter canceled or duplicated the shared build")
	}
	release <- struct{}{}
	payload, err := cache.getOrBuild(context.Background(), key, "permissions", build)
	if err != nil || payload["object"] != "fresh" || builds.Load() != 1 {
		t.Fatalf("second caller payload = %v, error = %v, builds = %d", payload, err, builds.Load())
	}
}

func TestModelsCacheCanceledRequestDoesNotStartBuild(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var builds atomic.Int32
	_, err := newModelsCache().getOrBuild(ctx, store.APIKey{ID: uuid.New()}, "", func(context.Context, store.APIKey) (map[string]any, error) {
		builds.Add(1)
		return nil, nil
	})
	if !errors.Is(err, context.Canceled) || builds.Load() != 0 {
		t.Fatalf("error = %v, builds = %d", err, builds.Load())
	}
}

func TestModelsPayloadCacheHitPermissionReadBudget(t *testing.T) {
	for _, tc := range []struct {
		name        string
		modelPolicy string
		sitePolicy  string
		models      int
		group       bool
		queries     int
	}{
		{"unrestricted", "allow_all", "allow_all", 0, false, 0},
		{"models_only", "allow_list", "allow_all", 100, false, 1},
		{"sites_only", "allow_all", "allow_list", 0, false, 2},
		{"sites_and_100_models", "allow_list", "allow_list", 100, false, 3},
		{"sites_and_10000_models", "allow_list", "allow_list", 10000, false, 3},
		{"group_and_models", "allow_list", "allow_list", 100, true, 5},
		{"no_allowed_models", "allow_list", "allow_list", 0, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := store.APIKey{ID: uuid.New(), SitePolicy: tc.sitePolicy, ModelPolicy: tc.modelPolicy}
			siteID, groupID, groupSiteID := uuid.New(), uuid.New(), uuid.New()
			permissions := make([]store.APIKeySiteModelPermission, tc.models)
			access := auth.APIKeyAccessSets{APIKey: key}
			for i := range permissions {
				id := uuid.New()
				permissions[i] = store.APIKeySiteModelPermission{SiteModelID: id, Enabled: true}
				access.AllowedSiteModelIDs = append(access.AllowedSiteModelIDs, id)
			}
			if tc.sitePolicy == "allow_list" && (tc.modelPolicy != "allow_list" || tc.models > 0) {
				access.AllowedSiteIDs = []uuid.UUID{siteID}
				if tc.group {
					access.AllowedSiteIDs = append(access.AllowedSiteIDs, groupSiteID)
				}
			}
			queries := 0
			db := gatewayGormWithQueryCallback(t, func(tx *gorm.DB) {
				queries++
				switch dest := tx.Statement.Dest.(type) {
				case *[]store.APIKeySiteModelPermission:
					*dest = permissions
				case *[]store.APIKeySitePermission:
					*dest = []store.APIKeySitePermission{{SiteID: siteID, Enabled: true}}
				case *[]store.APIKeySiteGroupPermission:
					if tc.group {
						*dest = []store.APIKeySiteGroupPermission{{GroupID: groupID, Enabled: true}}
					}
				case *[]store.SiteGroupSite:
					*dest = []store.SiteGroupSite{{GroupID: groupID, SiteID: groupSiteID}}
				case *[]store.SiteGroup:
					*dest = []store.SiteGroup{{ID: groupID, Enabled: true}}
				default:
					tx.AddError(fmt.Errorf("unexpected query %T", dest))
				}
			})
			h := Handler{auth: auth.NewService(db, "test-master-key"), modelsCache: newModelsCache()}
			h.modelsCache.items[key.ID] = modelsCacheEntry{
				revision: modelsAccessRevision(access), cached: time.Now(), payload: map[string]any{"object": "cached"},
			}
			payload, err := h.modelsPayloadForAPIKey(context.Background(), key)
			if err != nil || payload["object"] != "cached" || queries != tc.queries {
				t.Fatalf("payload = %v, error = %v, queries = %d, want %d", payload, err, queries, tc.queries)
			}
		})
	}
}

func TestModelsPayloadRebuildIntersectsSiteAndModelPermissions(t *testing.T) {
	key := store.APIKey{ID: uuid.New(), SitePolicy: "allow_list", ModelPolicy: "allow_list"}
	siteIDs := []uuid.UUID{uuid.New(), uuid.New()}
	modelIDs := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	canonicalIDs := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	modelNames := []string{"allowed", "blocked-site", "blocked-model"}
	queries := 0
	db := gatewayGormWithQueryCallback(t, func(tx *gorm.DB) {
		queries++
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.APIKeySiteModelPermission:
			*dest = []store.APIKeySiteModelPermission{{SiteModelID: modelIDs[0], Enabled: true}, {SiteModelID: modelIDs[1], Enabled: true}}
		case *[]store.APIKeySitePermission:
			*dest = []store.APIKeySitePermission{{SiteID: siteIDs[0], Enabled: true}}
		case *[]store.APIKeySiteGroupPermission:
			*dest = nil
		case *[]store.CanonicalModel:
			for i, id := range canonicalIDs {
				*dest = append(*dest, store.CanonicalModel{ID: id, ModelKey: modelNames[i], Status: "active"})
			}
		case *[]store.SiteModel:
			for i, id := range modelIDs {
				*dest = append(*dest, store.SiteModel{ID: id, SiteID: siteIDs[i%2], Status: "active", CanonicalID: uuid.NullUUID{UUID: canonicalIDs[i], Valid: true}})
			}
		case *[]store.Site:
			for _, id := range siteIDs {
				*dest = append(*dest, store.Site{ID: id, Status: "active", Enabled: true, SiteType: "openai"})
			}
		case *[]store.SiteCredential:
			for _, id := range siteIDs {
				*dest = append(*dest, store.SiteCredential{ID: uuid.New(), SiteID: id, CredentialType: "api_key", Meta: store.JSON(`{"enabled":true}`)})
			}
		case *[]store.SiteAPIKeyState, *[]store.SiteAPIKeyModel, *[]store.RouteCooldown:
		default:
			tx.AddError(fmt.Errorf("unexpected query %T", dest))
		}
	})
	h := Handler{db: gatewayStoreWithGorm(t, db), auth: auth.NewService(db, "test-master-key"), modelsCache: newModelsCache()}
	payload, err := h.modelsPayloadForAPIKey(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	rows := payload["data"].([]map[string]any)
	if len(rows) != 1 || rows[0]["id"] != "allowed" {
		t.Fatalf("permission intersection returned %v", rows)
	}
	if queries != 10 {
		t.Fatalf("cold read queries = %d, want 3 permission plus 7 catalog reads", queries)
	}
	queries = 0
	if _, err := h.modelsPayloadForAPIKey(context.Background(), key); err != nil || queries != 3 {
		t.Fatalf("warm read queries = %d, error = %v", queries, err)
	}
}

func TestModelsCacheInvalidationStopsObsoleteDatabaseWork(t *testing.T) {
	for _, mode := range []string{"key", "all"} {
		t.Run(mode, func(t *testing.T) {
			cache := newModelsCache()
			key := store.APIKey{ID: uuid.New()}
			otherKey := store.APIKey{ID: uuid.New()}
			started := make(chan struct{})
			otherStarted := make(chan context.Context, 1)
			done := make(chan error, 1)
			otherDone := make(chan struct{})
			defer cache.invalidate()
			go func() {
				_, err := cache.getOrBuild(context.Background(), key, "old", func(ctx context.Context, _ store.APIKey) (map[string]any, error) {
					close(started)
					<-ctx.Done()
					return nil, ctx.Err()
				})
				done <- err
			}()
			go func() {
				defer close(otherDone)
				_, _ = cache.getOrBuild(context.Background(), otherKey, "old", func(ctx context.Context, _ store.APIKey) (map[string]any, error) {
					otherStarted <- ctx
					<-ctx.Done()
					return nil, ctx.Err()
				})
			}()
			<-started
			otherCtx := <-otherStarted
			if mode == "key" {
				cache.invalidateKey(key.ID)
				if otherCtx.Err() != nil {
					t.Fatal("key invalidation canceled another key's build")
				}
			} else {
				cache.invalidate()
				if otherCtx.Err() == nil {
					t.Fatal("global invalidation left an obsolete build running")
				}
			}
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("obsolete build error = %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("invalidation left obsolete database work running")
			}
			cache.invalidate()
			<-otherDone
		})
	}
}
