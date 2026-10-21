package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type refreshLeaseRow struct {
	leaseID string
	until   time.Time
}

func TestSiteCredentialRefreshLeaseRepositoryAcquireAndRelease(t *testing.T) {
	t.Parallel()

	credentialID := uuid.New()
	otherCredentialID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	rows := map[uuid.UUID]refreshLeaseRow{}
	var statements []string
	var acquiredUntil []time.Time
	requireStoreCallback(t, "raw", db.Callback().Raw().Replace("gorm:raw", func(tx *gorm.DB) {
		sqlText := tx.Statement.SQL.String()
		statements = append(statements, sqlText)
		if strings.Contains(strings.ToLower(sqlText), "now()") {
			tx.AddError(errors.New("lease sql calls now()"))
			return
		}
		lower := strings.ToLower(sqlText)
		switch {
		case strings.Contains(lower, "insert into site_credential_refresh_leases"):
			if !strings.Contains(lower, "on conflict (site_credential_id)") || !strings.Contains(lower, "lease_until <") {
				tx.AddError(errors.New("acquire sql is missing the conditional upsert"))
				return
			}
			if len(tx.Statement.Vars) != 4 {
				tx.AddError(errors.New("acquire sql did not bind credential, lease, until, and now"))
				return
			}
			id, idOK := tx.Statement.Vars[0].(uuid.UUID)
			leaseID, leaseOK := tx.Statement.Vars[1].(string)
			until, untilOK := tx.Statement.Vars[2].(time.Time)
			now, nowOK := tx.Statement.Vars[3].(time.Time)
			if !idOK || !leaseOK || !untilOK || !nowOK {
				tx.AddError(errors.New("acquire sql bound unexpected value types"))
				return
			}
			acquiredUntil = append(acquiredUntil, until)
			current, held := rows[id]
			if !held || current.until.Before(now) {
				rows[id] = refreshLeaseRow{leaseID: leaseID, until: until}
				tx.RowsAffected = 1
				return
			}
			tx.RowsAffected = 0
		case strings.Contains(lower, "delete from site_credential_refresh_leases"):
			if !strings.Contains(lower, "site_credential_id") || !strings.Contains(lower, "lease_id") || len(tx.Statement.Vars) != 2 {
				tx.AddError(errors.New("release sql must filter on credential id and lease id"))
				return
			}
			id, idOK := tx.Statement.Vars[0].(uuid.UUID)
			leaseID, leaseOK := tx.Statement.Vars[1].(string)
			if !idOK || !leaseOK {
				tx.AddError(errors.New("release sql bound unexpected value types"))
				return
			}
			current, held := rows[id]
			if held && current.leaseID == leaseID {
				delete(rows, id)
				tx.RowsAffected = 1
				return
			}
			tx.RowsAffected = 0
		default:
			tx.AddError(errors.New("unexpected lease sql"))
		}
	}))

	repo := NewSiteCredentialRefreshLeaseRepository(db)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	ttl := time.Minute
	ctx := context.Background()

	acquired, err := repo.TryAcquire(ctx, credentialID, "lease-a", now, ttl)
	if err != nil || !acquired {
		t.Fatalf("first TryAcquire = %v, %v, want acquired", acquired, err)
	}
	if len(acquiredUntil) != 1 || !acquiredUntil[0].Equal(now.Add(ttl)) {
		t.Fatalf("lease until = %v, want %v", acquiredUntil, now.Add(ttl))
	}
	otherAcquired, err := repo.TryAcquire(ctx, otherCredentialID, "lease-other", now, ttl)
	if err != nil || !otherAcquired {
		t.Fatalf("TryAcquire(other credential) = %v, %v, want acquired", otherAcquired, err)
	}
	held, err := repo.TryAcquire(ctx, credentialID, "lease-b", now.Add(30*time.Second), ttl)
	if err != nil || held {
		t.Fatalf("TryAcquire(while held) = %v, %v, want not acquired", held, err)
	}
	if err := repo.Release(ctx, credentialID, "lease-b"); err != nil {
		t.Fatalf("Release(other lease) error = %v", err)
	}
	stillHeld, err := repo.TryAcquire(ctx, credentialID, "lease-c", now.Add(30*time.Second), ttl)
	if err != nil || stillHeld {
		t.Fatalf("TryAcquire(after releasing another lease) = %v, %v, want not acquired", stillHeld, err)
	}
	equalBoundary, err := repo.TryAcquire(ctx, credentialID, "lease-d", now.Add(ttl), ttl)
	if err != nil || equalBoundary {
		t.Fatalf("TryAcquire(at lease_until) = %v, %v, want not acquired", equalBoundary, err)
	}
	takenOver, err := repo.TryAcquire(ctx, credentialID, "lease-e", now.Add(ttl+time.Second), ttl)
	if err != nil || !takenOver {
		t.Fatalf("TryAcquire(after expiry) = %v, %v, want acquired", takenOver, err)
	}
	if err := repo.Release(ctx, credentialID, "lease-a"); err != nil {
		t.Fatalf("Release(previous lease) error = %v", err)
	}
	afterStaleRelease, err := repo.TryAcquire(ctx, credentialID, "lease-f", now.Add(ttl+2*time.Second), ttl)
	if err != nil || afterStaleRelease {
		t.Fatalf("TryAcquire(after releasing the old lease id) = %v, %v, want not acquired", afterStaleRelease, err)
	}
	if err := repo.Release(ctx, credentialID, "lease-e"); err != nil {
		t.Fatalf("Release(current lease) error = %v", err)
	}
	released, err := repo.TryAcquire(ctx, credentialID, "lease-g", now.Add(ttl+2*time.Second), ttl)
	if err != nil || !released {
		t.Fatalf("TryAcquire(after release) = %v, %v, want acquired", released, err)
	}
	if len(statements) == 0 || !strings.Contains(strings.ToLower(statements[0]), "insert into site_credential_refresh_leases") {
		t.Fatalf("statements = %#v, want the conditional upsert first", statements)
	}
}

func TestSiteCredentialUpdateMetaAndSecretTouchOnlyTheirColumns(t *testing.T) {
	t.Parallel()

	credentialID := uuid.New()
	row := SiteCredential{
		ID:              credentialID,
		SiteID:          uuid.New(),
		CredentialType:  "grok_sso:acct",
		EncryptedSecret: "secret-v1",
		MaskedSecret:    "sk-***",
		DisplayName:     "pool-a",
		Meta:            JSON(`{"tier":"premium"}`),
	}
	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*SiteCredential)
		if !ok {
			tx.AddError(errors.New("unexpected site credential query destination"))
			return
		}
		*item = row
		tx.RowsAffected = 1
	})
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		values, ok := tx.Statement.Dest.(map[string]any)
		if !ok || len(values) != 1 {
			tx.AddError(errors.New("site credential update must write one column"))
			return
		}
		switch {
		case values["meta"] != nil:
			meta, ok := values["meta"].(JSON)
			if !ok {
				tx.AddError(errors.New("meta update has the wrong type"))
				return
			}
			row.Meta = meta
		case values["encrypted_secret"] != nil:
			secret, ok := values["encrypted_secret"].(string)
			if !ok {
				tx.AddError(errors.New("secret update has the wrong type"))
				return
			}
			row.EncryptedSecret = secret
		default:
			tx.AddError(errors.New("site credential update wrote an unexpected column"))
			return
		}
		tx.RowsAffected = 1
	})

	repo := NewSiteCredentialRepository(db)
	ctx := context.Background()
	metaUpdated, err := repo.UpdateMeta(ctx, credentialID, JSON(`{"reauth_required":true}`))
	if err != nil {
		t.Fatalf("UpdateMeta error = %v", err)
	}
	if metaUpdated.EncryptedSecret != "secret-v1" || metaUpdated.DisplayName != "pool-a" || metaUpdated.MaskedSecret != "sk-***" || metaUpdated.CredentialType != "grok_sso:acct" {
		t.Fatalf("UpdateMeta rewrote other columns: %#v", metaUpdated)
	}
	if string(metaUpdated.Meta) != `{"reauth_required":true}` {
		t.Fatalf("UpdateMeta meta = %s", metaUpdated.Meta)
	}
	secretUpdated, err := repo.UpdateSecret(ctx, credentialID, "secret-v2")
	if err != nil {
		t.Fatalf("UpdateSecret error = %v", err)
	}
	if secretUpdated.EncryptedSecret != "secret-v2" {
		t.Fatalf("UpdateSecret secret = %q", secretUpdated.EncryptedSecret)
	}
	if secretUpdated.DisplayName != "pool-a" || string(secretUpdated.Meta) != `{"reauth_required":true}` || secretUpdated.MaskedSecret != "sk-***" || secretUpdated.CredentialType != "grok_sso:acct" {
		t.Fatalf("UpdateSecret rewrote other columns: %#v", secretUpdated)
	}
}
