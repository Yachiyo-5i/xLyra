package site

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	oauthsvc "xlyra/server/internal/oauth"
	"xlyra/server/internal/store"
)

func TestCompleteLoginNewAccount(t *testing.T) {
	longToken := "abcdefghijklmnop"
	cases := []struct {
		name     string
		email    string
		token    string
		wantMask string
	}{
		{name: "email", email: "a@example.com", token: longToken, wantMask: "a@example.com"},
		{name: "token", email: "", token: longToken, wantMask: "abcd…mnop"},
		{name: "short_token", email: "", token: "shorttok", wantMask: "****"},
	}
	for _, kind := range loginKinds() {
		for _, tc := range cases {
			t.Run(string(kind)+"/"+tc.name, func(t *testing.T) {
				t.Parallel()

				f := newLoginFixture(t, kind)
				spec := f.identitySpec(tc.token, tc.email)
				view, err := f.complete(spec)
				if err != nil {
					t.Fatalf("complete login: %v\n%s", err, f.traceText())
				}
				accounts := f.accountsOnSite()
				if len(accounts) != 1 {
					t.Fatalf("account count = %d, want 1\n%s", len(accounts), f.traceText())
				}
				cred := accounts[0]
				if cred.ID != view.CredentialID {
					t.Fatalf("snapshot credential = %s, stored %s", view.CredentialID, cred.ID)
				}
				assertAccountCredentialType(t, kind, cred.CredentialType)
				if cred.MaskedSecret != tc.wantMask || view.MaskedToken != tc.wantMask {
					t.Fatalf("mask stored %q snapshot %q, want %q", cred.MaskedSecret, view.MaskedToken, tc.wantMask)
				}
				if !view.Enabled {
					t.Fatal("snapshot enabled = false")
				}
				meta := accountMetaMap(cred.Meta)
				if meta["enabled"] != true {
					t.Fatalf("meta enabled = %#v", meta["enabled"])
				}
				if _, ok := meta["reauth_required"]; ok {
					t.Fatalf("reauth_required = %#v", meta["reauth_required"])
				}
				assertLoginEmailMeta(t, meta, tc.email)
				assertLoginIdentityMeta(t, kind, meta, spec)
				if got := f.accessToken(t, cred); got != tc.token {
					t.Fatalf("stored access token = %q", got)
				}
			})
		}
	}
}

func TestCompleteLoginSameIdentity(t *testing.T) {
	for _, kind := range loginKinds() {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()

			f := newLoginFixture(t, kind)
			first := f.identitySpec("token-one-aaaaaa", "first@example.com")
			view, err := f.complete(first)
			if err != nil {
				t.Fatalf("first login: %v\n%s", err, f.traceText())
			}
			original := f.credentials[view.CredentialID]
			originalType := original.CredentialType
			meta := accountMetaMap(original.Meta)
			meta["reauth_required"] = true
			meta["enabled"] = false
			meta["note"] = "keep"
			encoded, err := json.Marshal(meta)
			if err != nil {
				t.Fatal(err)
			}
			original.Meta = encoded
			f.credentials[view.CredentialID] = original

			second := f.identitySpec("token-two-bbbbbb", "second@example.com")
			again, err := f.complete(second)
			if err != nil {
				t.Fatalf("second login: %v\n%s", err, f.traceText())
			}
			accounts := f.accountsOnSite()
			if len(accounts) != 1 {
				t.Fatalf("account count = %d, want 1", len(accounts))
			}
			cred := accounts[0]
			if cred.ID != view.CredentialID || again.CredentialID != view.CredentialID || cred.CredentialType != originalType {
				t.Fatalf("credential id/type = %s %s, want %s %s", cred.ID, cred.CredentialType, view.CredentialID, originalType)
			}
			if got := f.accessToken(t, cred); got != second.AccessToken {
				t.Fatalf("stored access token = %q, want updated token", got)
			}
			meta = accountMetaMap(cred.Meta)
			if meta["enabled"] != true {
				t.Fatalf("enabled = %#v", meta["enabled"])
			}
			if _, ok := meta["reauth_required"]; ok {
				t.Fatalf("reauth_required = %#v", meta["reauth_required"])
			}
			if meta["note"] != "keep" || meta["email"] != second.Email {
				t.Fatalf("meta = %#v", meta)
			}
			assertLoginIdentityMeta(t, kind, meta, second)
		})
	}
}

func TestCompleteLoginReauthorize(t *testing.T) {
	for _, kind := range loginKinds() {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()

			f := newLoginFixture(t, kind)
			seeded := f.identitySpec("old-token-aaaa", "old@example.com")
			if kind == accountPoolGrok {
				seeded.PrincipalID = "old-principal"
				seeded.TeamID = "old-team"
			} else {
				seeded.Subject = "old-subject"
			}
			id := f.seedAccount(t, seeded, map[string]any{
				"enabled":         false,
				"reauth_required": true,
				"note":            "keep",
			})
			f.states[id] = store.SiteAPIKeyState{
				SiteCredentialID: id,
				SiteID:           f.siteID,
				Enabled:          false,
				SyncStatus:       "reauth_required",
				SyncMessage:      sql.NullString{String: "needs reauth", Valid: true},
			}
			otherCredential := uuid.New()
			otherSite := uuid.New()
			f.cooldowns = []loginCooldown{
				{siteID: f.siteID, credentialID: id},
				{siteID: f.siteID, credentialID: otherCredential},
				{siteID: otherSite, credentialID: id},
			}
			originalType := f.credentials[id].CredentialType

			next := f.identitySpec("new-token-bbbb", "new@example.com")
			if kind == accountPoolGrok {
				next.PrincipalID = "new-principal"
				next.TeamID = "new-team"
			} else {
				next.Subject = "new-subject"
			}
			next.Target = &id
			view, err := f.complete(next)
			if err != nil {
				t.Fatalf("reauthorize: %v\n%s", err, f.traceText())
			}
			if len(f.accountsOnSite()) != 1 {
				t.Fatalf("account count = %d, want 1", len(f.accountsOnSite()))
			}
			cred := f.credentials[id]
			if view.CredentialID != id || cred.CredentialType != originalType {
				t.Fatalf("credential = %s %s, want %s %s", view.CredentialID, cred.CredentialType, id, originalType)
			}
			if got := f.accessToken(t, cred); got != next.AccessToken {
				t.Fatalf("stored access token = %q", got)
			}
			meta := accountMetaMap(cred.Meta)
			if meta["enabled"] != true || meta["note"] != "keep" {
				t.Fatalf("meta = %#v", meta)
			}
			if _, ok := meta["reauth_required"]; ok {
				t.Fatalf("reauth_required = %#v", meta["reauth_required"])
			}
			assertLoginIdentityMeta(t, kind, meta, next)
			if !view.Enabled || view.SyncStatus != "pending" {
				t.Fatalf("snapshot enabled=%v sync=%q", view.Enabled, view.SyncStatus)
			}
			state := f.states[id]
			if state.SyncStatus != "pending" || !state.Enabled || state.SyncMessage.Valid {
				t.Fatalf("state = %#v", state)
			}
			if len(f.cooldowns) != 2 || !f.hasCooldown(f.siteID, otherCredential) || !f.hasCooldown(otherSite, id) || f.hasCooldown(f.siteID, id) {
				t.Fatalf("cooldowns = %#v", f.cooldowns)
			}
		})
	}
}

func TestCompleteLoginIdentityFallback(t *testing.T) {
	for _, kind := range loginKinds() {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()

			f := newLoginFixture(t, kind)
			spec := f.identitySpec("new-token-cccc", "new@example.com")
			var claims map[string]any
			if kind == accountPoolGrok {
				claims = map[string]any{"principal_id": spec.PrincipalID, "team_id": spec.TeamID}
			} else {
				claims = map[string]any{"uid": spec.Subject}
			}
			matchID := f.seedAccount(t, loginSpec{AccessToken: loginJWT(t, claims)}, map[string]any{
				"email": "old@example.com",
				"note":  "keep",
			})
			decoy := f.identitySpec("decoy-token-dddd", "decoy@example.com")
			if kind == accountPoolGrok {
				decoy.PrincipalID = "other-principal"
			} else {
				decoy.Subject = "other-subject"
			}
			decoyID := f.seedAccount(t, decoy, map[string]any{
				"email":        "decoy@example.com",
				"principal_id": decoy.PrincipalID,
				"team_id":      decoy.TeamID,
				"subject":      decoy.Subject,
			})
			originalType := f.credentials[matchID].CredentialType

			view, err := f.complete(spec)
			if err != nil {
				t.Fatalf("fallback login: %v\n%s", err, f.traceText())
			}
			if len(f.accountsOnSite()) != 2 {
				t.Fatalf("account count = %d, want 2", len(f.accountsOnSite()))
			}
			if view.CredentialID != matchID || f.credentials[matchID].CredentialType != originalType {
				t.Fatalf("matched %s type %s, want %s %s", view.CredentialID, f.credentials[matchID].CredentialType, matchID, originalType)
			}
			if got := f.accessToken(t, f.credentials[matchID]); got != spec.AccessToken {
				t.Fatalf("matched token = %q", got)
			}
			if got := f.accessToken(t, f.credentials[decoyID]); got != decoy.AccessToken {
				t.Fatalf("decoy token = %q", got)
			}
			meta := accountMetaMap(f.credentials[matchID].Meta)
			if meta["note"] != "keep" || meta["enabled"] != true {
				t.Fatalf("meta = %#v", meta)
			}
			assertLoginIdentityMeta(t, kind, meta, spec)
		})
	}
}

func TestCompleteLoginErrors(t *testing.T) {
	for _, kind := range loginKinds() {
		kind := kind
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()

			tokenError := fmt.Sprintf("%s tokens are missing an access token", kind)
			notFound := fmt.Sprintf("%s account was not found", kind)
			cases := []struct {
				name     string
				siteType string
				token    string
				target   string
				want     string
			}{
				{name: "wrong_site_type", siteType: "openai", token: "abcdefghijklmnop", want: fmt.Sprintf("site type %q is not %s", "openai", kind)},
				{name: "site_type_before_token", siteType: "openai", token: "", want: fmt.Sprintf("site type %q is not %s", "openai", kind)},
				{name: "empty_access_token", token: "", want: tokenError},
				{name: "blank_access_token", token: " \t ", want: tokenError},
				{name: "target_other_site", token: "abcdefghijklmnop", target: "other_site", want: notFound},
				{name: "target_wrong_type", token: "abcdefghijklmnop", target: "wrong_type", want: notFound},
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					f := newLoginFixture(t, kind)
					if tc.siteType != "" {
						f.site.SiteType = tc.siteType
					}
					spec := f.identitySpec(tc.token, "a@example.com")
					spec.AccessToken = tc.token
					switch tc.target {
					case "other_site":
						id := uuid.New()
						f.credentials[id] = siteEncryptedCredential(t, id, uuid.New(), kind.credentialPrefix()+uuid.NewString(), "secret", siteJSONMeta(t, map[string]any{}))
						spec.Target = &id
					case "wrong_type":
						id := uuid.New()
						f.credentials[id] = siteEncryptedCredential(t, id, f.siteID, "api_key:"+uuid.NewString(), "secret", siteJSONMeta(t, map[string]any{}))
						spec.Target = &id
					}
					before := len(f.accountsOnSite())
					_, err := f.complete(spec)
					if err == nil || err.Error() != tc.want {
						t.Fatalf("error = %v, want %q\n%s", err, tc.want, f.traceText())
					}
					if len(f.accountsOnSite()) != before {
						t.Fatalf("account count changed from %d to %d", before, len(f.accountsOnSite()))
					}
				})
			}
		})
	}
}

func TestCompleteLoginGrokMissingIdentity(t *testing.T) {
	t.Parallel()

	t.Run("absent", func(t *testing.T) {
		f := newLoginFixture(t, accountPoolGrok)
		spec := loginSpec{AccessToken: "abcdefghijklmnop", Tier: "premium"}
		first, err := f.complete(spec)
		if err != nil {
			t.Fatalf("first login: %v\n%s", err, f.traceText())
		}
		second, err := f.complete(spec)
		if err != nil {
			t.Fatalf("second login: %v\n%s", err, f.traceText())
		}
		if first.CredentialID == second.CredentialID || len(f.accountsOnSite()) != 2 {
			t.Fatalf("ids %s %s count %d, want two credentials", first.CredentialID, second.CredentialID, len(f.accountsOnSite()))
		}
		meta := accountMetaMap(f.credentials[first.CredentialID].Meta)
		if _, ok := meta["principal_id"]; ok {
			t.Fatalf("principal_id = %#v", meta["principal_id"])
		}
		if _, ok := meta["team_id"]; ok {
			t.Fatalf("team_id = %#v", meta["team_id"])
		}
		if meta["tier"] != "premium" || meta["enabled"] != true {
			t.Fatalf("meta = %#v", meta)
		}
	})

	t.Run("from_token", func(t *testing.T) {
		f := newLoginFixture(t, accountPoolGrok)
		token := loginJWT(t, map[string]any{"principal_id": "principal-1", "team_id": "team-1"})
		spec := loginSpec{AccessToken: token, Tier: "premium"}
		first, err := f.complete(spec)
		if err != nil {
			t.Fatalf("first login: %v\n%s", err, f.traceText())
		}
		second, err := f.complete(spec)
		if err != nil {
			t.Fatalf("second login: %v\n%s", err, f.traceText())
		}
		if first.CredentialID != second.CredentialID || len(f.accountsOnSite()) != 1 {
			t.Fatalf("ids %s %s count %d, want one credential", first.CredentialID, second.CredentialID, len(f.accountsOnSite()))
		}
		meta := accountMetaMap(f.credentials[first.CredentialID].Meta)
		if meta["principal_id"] != "principal-1" || meta["team_id"] != "team-1" {
			t.Fatalf("meta = %#v", meta)
		}
	})

	t.Run("team_absent", func(t *testing.T) {
		f := newLoginFixture(t, accountPoolGrok)
		spec := loginSpec{AccessToken: "abcdefghijklmnop", PrincipalID: "principal-1", Tier: "premium"}
		first, err := f.complete(spec)
		if err != nil {
			t.Fatalf("first login: %v\n%s", err, f.traceText())
		}
		second, err := f.complete(spec)
		if err != nil {
			t.Fatalf("second login: %v\n%s", err, f.traceText())
		}
		if first.CredentialID != second.CredentialID || len(f.accountsOnSite()) != 1 {
			t.Fatalf("same principal without a team created %d credentials", len(f.accountsOnSite()))
		}
		meta := accountMetaMap(f.credentials[first.CredentialID].Meta)
		if meta["principal_id"] != "principal-1" {
			t.Fatalf("principal_id = %#v", meta["principal_id"])
		}
		if _, ok := meta["team_id"]; ok {
			t.Fatalf("team_id = %#v", meta["team_id"])
		}
		spec.TeamID = "team-2"
		third, err := f.complete(spec)
		if err != nil {
			t.Fatalf("other team: %v\n%s", err, f.traceText())
		}
		if third.CredentialID == first.CredentialID || len(f.accountsOnSite()) != 2 {
			t.Fatalf("other team id %s count %d, want a new credential", third.CredentialID, len(f.accountsOnSite()))
		}
	})
}

func loginKinds() []accountPoolKind {
	return []accountPoolKind{accountPoolGrok}
}

func assertAccountCredentialType(t *testing.T, kind accountPoolKind, credentialType string) {
	t.Helper()

	prefix := kind.credentialPrefix()
	rest := strings.TrimPrefix(credentialType, prefix)
	if rest == credentialType {
		t.Fatalf("credential type %q missing prefix %s", credentialType, prefix)
	}
	if _, err := uuid.Parse(rest); err != nil {
		t.Fatalf("credential type %q: %v", credentialType, err)
	}
}

func assertLoginEmailMeta(t *testing.T, meta map[string]any, email string) {
	t.Helper()

	if email == "" {
		if _, ok := meta["email"]; ok {
			t.Fatalf("email = %#v, want omitted", meta["email"])
		}
		return
	}
	if meta["email"] != email {
		t.Fatalf("email = %#v, want %q", meta["email"], email)
	}
}

func assertLoginIdentityMeta(t *testing.T, kind accountPoolKind, meta map[string]any, spec loginSpec) {
	t.Helper()

	if meta["principal_id"] != spec.PrincipalID || meta["team_id"] != spec.TeamID || meta["tier"] != spec.Tier {
		t.Fatalf("identity meta = %#v, want principal %q team %q tier %q", meta, spec.PrincipalID, spec.TeamID, spec.Tier)
	}
}

func loginJWT(t *testing.T, claims map[string]any) string {
	t.Helper()

	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

type loginSpec struct {
	AccessToken string
	Email       string
	PrincipalID string
	TeamID      string
	Tier        string
	Subject     string
	Domain      string
	Realm       string
	Target      *uuid.UUID
}

type loginView struct {
	CredentialID uuid.UUID
	MaskedToken  string
	Enabled      bool
	SyncStatus   string
	Region       string
}

type loginCooldown struct {
	siteID       uuid.UUID
	credentialID uuid.UUID
}

type loginFixture struct {
	t           *testing.T
	kind        accountPoolKind
	service     *Service
	siteID      uuid.UUID
	site        store.Site
	credentials map[uuid.UUID]store.SiteCredential
	states      map[uuid.UUID]store.SiteAPIKeyState
	cooldowns   []loginCooldown
	trace       []string
}

func newLoginFixture(t *testing.T, kind accountPoolKind) *loginFixture {
	t.Helper()

	f := &loginFixture{
		t:           t,
		kind:        kind,
		siteID:      uuid.New(),
		credentials: map[uuid.UUID]store.SiteCredential{},
		states:      map[uuid.UUID]store.SiteAPIKeyState{},
	}
	f.site = store.Site{
		ID:       f.siteID,
		Name:     string(kind),
		SiteType: kind.siteType(),
		BaseURL:  "https://example.test",
		Enabled:  true,
	}
	db, _ := grokTransactionalGorm(t, siteGormCallbacks{
		query:          f.query,
		create:         f.create,
		update:         f.update,
		deleteCallback: f.delete,
	})
	f.service = NewService(siteStoreWithGorm(t, db), siteTestMasterKey)
	return f
}

func (f *loginFixture) identitySpec(access string, email string) loginSpec {
	spec := loginSpec{AccessToken: access, Email: email}
	spec.PrincipalID = "principal-1"
	spec.TeamID = "team-1"
	spec.Tier = "premium"
	return spec
}

func (f *loginFixture) complete(spec loginSpec) (loginView, error) {
	ctx := context.Background()
	account, err := f.service.CompleteGrokDeviceLogin(ctx, f.siteID, oauthsvc.GrokTokens{
		AccessToken: spec.AccessToken,
		Email:       spec.Email,
		PrincipalID: spec.PrincipalID,
		TeamID:      spec.TeamID,
		Tier:        spec.Tier,
	}, spec.Target)
	if err != nil {
		return loginView{}, err
	}
	return loginView{
		CredentialID: account.CredentialID,
		MaskedToken:  account.MaskedToken,
		Enabled:      account.Enabled,
		SyncStatus:   account.SyncStatus,
	}, nil
}

func (f *loginFixture) seedAccount(t *testing.T, spec loginSpec, meta map[string]any) uuid.UUID {
	t.Helper()

	secret, err := oauthsvc.EncodeGrokBundle(oauthsvc.GrokCredentialBundle{
		AccessToken:  spec.AccessToken,
		RefreshToken: "refresh-old",
		ExpiresAt:    9999999999,
	})
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	stored := meta
	if spec.PrincipalID != "" {
		stored["principal_id"] = spec.PrincipalID
	}
	if spec.TeamID != "" {
		stored["team_id"] = spec.TeamID
	}
	f.credentials[id] = siteEncryptedCredential(t, id, f.siteID, f.kind.credentialPrefix()+uuid.NewString(), secret, siteJSONMeta(t, stored))
	return id
}

func (f *loginFixture) accessToken(t *testing.T, cred store.SiteCredential) string {
	t.Helper()

	plain, err := f.service.credentials.Decrypt(cred.EncryptedSecret)
	if err != nil {
		t.Fatalf("decrypt credential: %v", err)
	}
	bundle, err := oauthsvc.DecodeGrokBundle(plain)
	if err != nil {
		t.Fatal(err)
	}
	return bundle.AccessToken
}

func (f *loginFixture) accountsOnSite() []store.SiteCredential {
	var out []store.SiteCredential
	for _, cred := range f.credentials {
		if cred.SiteID == f.siteID && f.isAccount(cred.CredentialType) {
			out = append(out, cred)
		}
	}
	return out
}

func (f *loginFixture) isAccount(credentialType string) bool {
	return isGrokCredentialType(credentialType)
}

func (f *loginFixture) hasCooldown(siteID uuid.UUID, credentialID uuid.UUID) bool {
	for _, item := range f.cooldowns {
		if item.siteID == siteID && item.credentialID == credentialID {
			return true
		}
	}
	return false
}

func (f *loginFixture) traceText() string {
	return strings.Join(f.trace, "\n")
}

func (f *loginFixture) note(format string, args ...any) {
	f.trace = append(f.trace, fmt.Sprintf(format, args...))
}

func (f *loginFixture) rows(tx *gorm.DB, n int) {
	tx.RowsAffected = int64(n)
	tx.Statement.RowsAffected = int64(n)
}

func (f *loginFixture) query(tx *gorm.DB) {
	switch dest := tx.Statement.Dest.(type) {
	case *store.Site:
		ids, _ := clauseValues(tx)
		if len(ids) > 0 && !containsLoginID(ids, f.site.ID) {
			tx.AddError(gorm.ErrRecordNotFound)
			return
		}
		*dest = f.site
		f.rows(tx, 1)
	case *store.SiteCredential:
		cred, ok := f.findCredential(tx)
		if !ok {
			tx.AddError(gorm.ErrRecordNotFound)
			return
		}
		*dest = cred
		f.rows(tx, 1)
	case *[]store.SiteCredential:
		ids, _ := clauseValues(tx)
		values := make([]store.SiteCredential, 0)
		for _, cred := range f.credentials {
			if len(ids) == 0 || containsLoginID(ids, cred.SiteID) {
				values = append(values, cred)
			}
		}
		*dest = values
		f.rows(tx, len(values))
	case *store.SiteAPIKeyState:
		id := f.stateID(tx)
		state, ok := f.states[id]
		if !ok {
			tx.AddError(gorm.ErrRecordNotFound)
			return
		}
		*dest = state
		f.rows(tx, 1)
	case *[]store.SiteAPIKeyState:
		ids, _ := clauseValues(tx)
		values := make([]store.SiteAPIKeyState, 0)
		for _, state := range f.states {
			if len(ids) == 0 || containsLoginID(ids, state.SiteID) {
				values = append(values, state)
			}
		}
		*dest = values
		f.rows(tx, len(values))
	case *[]store.SiteAPIKeyModel:
		*dest = []store.SiteAPIKeyModel{}
		f.rows(tx, 0)
	default:
		f.note("query %T", tx.Statement.Dest)
		tx.AddError(fmt.Errorf("unhandled query %T", tx.Statement.Dest))
	}
}

func (f *loginFixture) create(tx *gorm.DB) {
	switch dest := tx.Statement.Dest.(type) {
	case *store.SiteCredential:
		if dest.ID == uuid.Nil {
			dest.ID = uuid.New()
		}
		f.credentials[dest.ID] = *dest
	case *store.SiteAPIKeyState:
		f.states[dest.SiteCredentialID] = *dest
	default:
		f.note("create %T", tx.Statement.Dest)
		tx.AddError(fmt.Errorf("unhandled create %T", tx.Statement.Dest))
		return
	}
	f.rows(tx, 1)
}

func (f *loginFixture) update(tx *gorm.DB) {
	switch dest := tx.Statement.Dest.(type) {
	case *store.SiteCredential:
		if dest.ID == uuid.Nil {
			tx.AddError(fmt.Errorf("credential update missing id"))
			return
		}
		f.credentials[dest.ID] = *dest
	case *store.SiteAPIKeyState:
		if dest.SiteCredentialID == uuid.Nil {
			tx.AddError(fmt.Errorf("state update missing credential id"))
			return
		}
		f.states[dest.SiteCredentialID] = *dest
	default:
		f.note("update %T %s", tx.Statement.Dest, loginClauseDebug(tx))
		tx.AddError(fmt.Errorf("unhandled update %T", tx.Statement.Dest))
		return
	}
	f.rows(tx, 1)
}

func (f *loginFixture) delete(tx *gorm.DB) {
	switch loginStatementModel(tx).(type) {
	case *store.RouteCooldown, store.RouteCooldown:
		f.deleteCooldowns(tx)
	default:
		f.note("delete %T %s", loginStatementModel(tx), loginClauseDebug(tx))
		tx.AddError(fmt.Errorf("unhandled delete %T", loginStatementModel(tx)))
		return
	}
	f.rows(tx, 1)
}

func (f *loginFixture) deleteCooldowns(tx *gorm.DB) {
	ids := loginClauseIDs(tx)
	f.note("delete cooldowns %s ids=%v", loginClauseDebug(tx), ids)
	kept := make([]loginCooldown, 0, len(f.cooldowns))
	for _, item := range f.cooldowns {
		if containsLoginID(ids, item.credentialID) && containsLoginID(ids, item.siteID) {
			continue
		}
		kept = append(kept, item)
	}
	f.cooldowns = kept
}

func (f *loginFixture) findCredential(tx *gorm.DB) (store.SiteCredential, bool) {
	ids, texts := clauseValues(tx)
	for _, id := range ids {
		if cred, ok := f.credentials[id]; ok {
			return cred, true
		}
	}
	credentialType := ""
	for _, text := range texts {
		if text != "" {
			credentialType = text
			break
		}
	}
	if credentialType == "" {
		return store.SiteCredential{}, false
	}
	for _, cred := range f.credentials {
		if cred.CredentialType != credentialType {
			continue
		}
		if len(ids) > 0 && !containsLoginID(ids, cred.SiteID) {
			continue
		}
		return cred, true
	}
	return store.SiteCredential{}, false
}

func (f *loginFixture) stateID(tx *gorm.DB) uuid.UUID {
	for _, id := range loginClauseIDs(tx) {
		if _, ok := f.states[id]; ok {
			return id
		}
		if _, ok := f.credentials[id]; ok {
			return id
		}
	}
	return uuid.Nil
}

func containsLoginID(ids []uuid.UUID, want uuid.UUID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func loginStatementModel(tx *gorm.DB) any {
	if tx.Statement != nil && tx.Statement.Model != nil {
		return tx.Statement.Model
	}
	if tx.Statement != nil {
		return tx.Statement.Dest
	}
	return nil
}

func loginClauseIDs(tx *gorm.DB) []uuid.UUID {
	ids, texts := clauseValues(tx)
	for _, text := range texts {
		if id, err := uuid.Parse(text); err == nil {
			ids = append(ids, id)
		}
	}
	if tx.Statement == nil {
		return ids
	}
	if where, ok := tx.Statement.Clauses["WHERE"].Expression.(clause.Where); ok {
		collectLoginIDs(where, &ids)
	}
	return ids
}

func collectLoginIDs(expr clause.Expression, ids *[]uuid.UUID) {
	if expr == nil {
		return
	}
	switch typed := expr.(type) {
	case clause.Where:
		for _, item := range typed.Exprs {
			collectLoginIDs(item, ids)
		}
	case clause.AndConditions:
		for _, item := range typed.Exprs {
			collectLoginIDs(item, ids)
		}
	case clause.OrConditions:
		for _, item := range typed.Exprs {
			collectLoginIDs(item, ids)
		}
	case clause.Eq:
		appendLoginID(typed.Value, ids)
	case clause.Expr:
		for _, value := range typed.Vars {
			appendLoginID(value, ids)
		}
	case clause.NamedExpr:
		for _, value := range typed.Vars {
			appendLoginID(value, ids)
		}
	}
}

func appendLoginID(value any, ids *[]uuid.UUID) {
	switch typed := value.(type) {
	case uuid.UUID:
		*ids = append(*ids, typed)
	case *uuid.UUID:
		if typed != nil {
			*ids = append(*ids, *typed)
		}
	case uuid.NullUUID:
		if typed.Valid {
			*ids = append(*ids, typed.UUID)
		}
	case *uuid.NullUUID:
		if typed != nil && typed.Valid {
			*ids = append(*ids, typed.UUID)
		}
	case string:
		if id, err := uuid.Parse(typed); err == nil {
			*ids = append(*ids, id)
		}
	}
}

func loginClauseDebug(tx *gorm.DB) string {
	if tx.Statement == nil {
		return ""
	}
	where, ok := tx.Statement.Clauses["WHERE"].Expression.(clause.Where)
	if !ok {
		return fmt.Sprintf("where=%T", tx.Statement.Clauses["WHERE"].Expression)
	}
	var b strings.Builder
	var walk func(clause.Expression)
	walk = func(expr clause.Expression) {
		if expr == nil {
			return
		}
		switch typed := expr.(type) {
		case clause.Where:
			for _, item := range typed.Exprs {
				walk(item)
			}
		case clause.AndConditions:
			for _, item := range typed.Exprs {
				walk(item)
			}
		case clause.Eq:
			fmt.Fprintf(&b, "%v=%T ", typed.Column, typed.Value)
		default:
			fmt.Fprintf(&b, "%T ", expr)
		}
	}
	walk(where)
	return b.String()
}
