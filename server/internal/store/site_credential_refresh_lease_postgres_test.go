package store

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPostgresSiteCredentialRefreshLeaseAndColumnUpdates(t *testing.T) {
	db, cfg, cleanup := openTemporaryMigrationStore(t)
	defer cleanup()
	ctx := context.Background()
	if err := ensureDatabaseInitializedOnce(ctx, cfg); err != nil {
		t.Fatalf("initialize schema: %v", err)
	}

	site, err := NewSiteRepository(db).Create(ctx, CreateSiteParams{
		Name:            "lease coverage",
		Slug:            "lease-coverage-" + uuid.NewString(),
		SiteType:        "grok",
		BaseURL:         "https://example.invalid",
		Status:          "active",
		Enabled:         true,
		RoutingPriority: 1,
	})
	if err != nil {
		t.Fatalf("create site: %v", err)
	}
	repo := NewSiteCredentialRepository(db)
	created, err := repo.Upsert(ctx, UpsertSiteCredentialParams{
		SiteID:          site.ID,
		CredentialType:  "grok_sso:" + uuid.NewString(),
		EncryptedSecret: "secret-v1",
		MaskedSecret:    "sk-***",
		Meta:            JSON(`{"tier":"premium"}`),
	})
	if err != nil {
		t.Fatalf("create credential: %v", err)
	}
	displayName := "pool-a"
	if _, err := repo.UpdateRoutingConfig(ctx, created.ID, UpdateSiteCredentialRoutingConfigParams{DisplayName: &displayName}); err != nil {
		t.Fatalf("set display name: %v", err)
	}

	leases := NewSiteCredentialRefreshLeaseRepository(db)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	ttl := time.Minute
	acquired, err := leases.TryAcquire(ctx, created.ID, "lease-a", now, ttl)
	if err != nil || !acquired {
		t.Fatalf("first TryAcquire = %v, %v, want acquired", acquired, err)
	}
	held, err := leases.TryAcquire(ctx, created.ID, "lease-b", now.Add(30*time.Second), ttl)
	if err != nil || held {
		t.Fatalf("TryAcquire(while held) = %v, %v, want not acquired", held, err)
	}
	if err := leases.Release(ctx, created.ID, "lease-b"); err != nil {
		t.Fatalf("Release(other lease) error = %v", err)
	}
	stillHeld, err := leases.TryAcquire(ctx, created.ID, "lease-c", now.Add(30*time.Second), ttl)
	if err != nil || stillHeld {
		t.Fatalf("TryAcquire(after releasing another lease) = %v, %v, want not acquired", stillHeld, err)
	}
	equalBoundary, err := leases.TryAcquire(ctx, created.ID, "lease-d", now.Add(ttl), ttl)
	if err != nil || equalBoundary {
		t.Fatalf("TryAcquire(at lease_until) = %v, %v, want not acquired", equalBoundary, err)
	}
	takenOver, err := leases.TryAcquire(ctx, created.ID, "lease-e", now.Add(ttl+time.Second), ttl)
	if err != nil || !takenOver {
		t.Fatalf("TryAcquire(after expiry) = %v, %v, want acquired", takenOver, err)
	}
	if err := leases.Release(ctx, created.ID, "lease-a"); err != nil {
		t.Fatalf("Release(previous lease) error = %v", err)
	}
	afterStaleRelease, err := leases.TryAcquire(ctx, created.ID, "lease-f", now.Add(ttl+2*time.Second), ttl)
	if err != nil || afterStaleRelease {
		t.Fatalf("TryAcquire(after releasing the old lease id) = %v, %v, want not acquired", afterStaleRelease, err)
	}
	if err := leases.Release(ctx, created.ID, "lease-e"); err != nil {
		t.Fatalf("Release(current lease) error = %v", err)
	}
	released, err := leases.TryAcquire(ctx, created.ID, "lease-g", now.Add(ttl+2*time.Second), ttl)
	if err != nil || !released {
		t.Fatalf("TryAcquire(after release) = %v, %v, want acquired", released, err)
	}
	if err := leases.Release(ctx, created.ID, "lease-g"); err != nil {
		t.Fatalf("Release(final lease) error = %v", err)
	}

	metaUpdated, err := repo.UpdateMeta(ctx, created.ID, JSON(`{"reauth_required":true}`))
	if err != nil {
		t.Fatalf("UpdateMeta error = %v", err)
	}
	if metaUpdated.EncryptedSecret != "secret-v1" || metaUpdated.DisplayName != "pool-a" || metaUpdated.MaskedSecret != "sk-***" || metaUpdated.CredentialType != created.CredentialType {
		t.Fatalf("UpdateMeta rewrote other columns: %#v", metaUpdated)
	}
	assertJSONEqual(t, metaUpdated.Meta, `{"reauth_required":true}`)
	reloaded, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID after UpdateMeta error = %v", err)
	}
	if reloaded.EncryptedSecret != "secret-v1" || reloaded.DisplayName != "pool-a" {
		t.Fatalf("stored row after UpdateMeta = %#v", reloaded)
	}

	secretUpdated, err := repo.UpdateSecret(ctx, created.ID, "secret-v2")
	if err != nil {
		t.Fatalf("UpdateSecret error = %v", err)
	}
	if secretUpdated.EncryptedSecret != "secret-v2" || secretUpdated.DisplayName != "pool-a" || secretUpdated.MaskedSecret != "sk-***" || secretUpdated.CredentialType != created.CredentialType {
		t.Fatalf("UpdateSecret rewrote other columns: %#v", secretUpdated)
	}
	assertJSONEqual(t, secretUpdated.Meta, `{"reauth_required":true}`)
	reloaded, err = repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID after UpdateSecret error = %v", err)
	}
	if reloaded.EncryptedSecret != "secret-v2" || reloaded.DisplayName != "pool-a" {
		t.Fatalf("stored row after UpdateSecret = %#v", reloaded)
	}
	assertJSONEqual(t, reloaded.Meta, `{"reauth_required":true}`)
}

func TestPostgresUpdateSecretIfTypePrefix(t *testing.T) {
	db, cfg, cleanup := openTemporaryMigrationStore(t)
	defer cleanup()
	ctx := context.Background()
	if err := ensureDatabaseInitializedOnce(ctx, cfg); err != nil {
		t.Fatalf("initialize schema: %v", err)
	}
	site, err := NewSiteRepository(db).Create(ctx, CreateSiteParams{
		Name: "conditional secret", Slug: "cond-secret-" + uuid.NewString(),
		SiteType: "grok", BaseURL: "https://example.invalid", Status: "active", Enabled: true, RoutingPriority: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	repo := NewSiteCredentialRepository(db)
	created, err := repo.Upsert(ctx, UpsertSiteCredentialParams{
		SiteID: site.ID, CredentialType: CredentialPrefixGrok + uuid.NewString(),
		EncryptedSecret: "bundle-v1", MaskedSecret: "gk", Meta: JSON(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := repo.UpdateSecretIfTypePrefix(ctx, created.ID, "bundle-v2", CredentialPrefixGrok)
	if err != nil {
		t.Fatal(err)
	}
	if updated.EncryptedSecret != "bundle-v2" || updated.CredentialType != created.CredentialType {
		t.Fatalf("conditional update = %#v", updated)
	}
	if _, err := repo.UpdateCredentialType(ctx, created.ID, NewAccountCredentialType("acme.example/pool", created.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpdateSecretIfTypePrefix(ctx, created.ID, "bundle-v3", CredentialPrefixGrok); !errors.Is(err, ErrCredentialTypeMismatch) {
		t.Fatalf("mismatched type error = %v", err)
	}
	reloaded, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.EncryptedSecret != "bundle-v2" {
		t.Fatalf("secret after rejected update = %q", reloaded.EncryptedSecret)
	}
}

func assertJSONEqual(t *testing.T, got JSON, want string) {
	t.Helper()
	var left any
	var right any
	if err := json.Unmarshal(got, &left); err != nil {
		t.Fatalf("decode stored json %s: %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &right); err != nil {
		t.Fatalf("decode expected json %s: %v", want, err)
	}
	if !reflect.DeepEqual(left, right) {
		t.Fatalf("json = %s, want %s", got, want)
	}
}
