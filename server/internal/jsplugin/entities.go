package jsplugin

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

// Built-in object types an automation input can pick. Each is registered once
// below, with what xLyra needs to offer it in a form, check a pick, and show it
// to a plugin. Adding a type is adding an entry here (plus whatever events and
// actions should involve it, which stay a decision of their own).
const (
	EntityOAuthConnection = "oauth_connection"
	EntityAPIKey          = "api_key"
)

// EntityRequireFiniteTotalQuota is the requirement that an API key has a finite
// total quota, so there is something to reset.
const EntityRequireFiniteTotalQuota = "finite_total_quota"

// Reason codes an object can be ruled out for.
const (
	ReasonProviderNotSupported = "provider_not_supported"
	ReasonNoFiniteTotalQuota   = "no_finite_total_quota"
)

type entityKind struct {
	Name string
	// SupportsProviders reports whether an input may restrict the object by provider.
	SupportsProviders bool
	// Requirements are the values an input's requires may take.
	Requirements []string
	// List returns every object of the type, for the picker.
	List func(ctx context.Context, db *store.Store) ([]AutomationEntity, error)
	// Resolve returns the objects with these ids; ids that do not exist are absent.
	Resolve func(ctx context.Context, db *store.Store, ids []string) ([]AutomationEntity, error)
	// Check says why an object does not satisfy an input: a stable code and an
	// English sentence, both empty when it does.
	Check func(entity AutomationEntity, input AutomationInputSpec) (code, reason string)
	// Describe is a short line shown under an option in the picker.
	Describe func(entity AutomationEntity) string
	// Snapshot is the object's state for a schedule.tick event.
	Snapshot func(ctx context.Context, db *store.Store, id string) (map[string]any, error)
}

var entityKinds = []entityKind{oauthConnectionKind, apiKeyKind}

func lookupEntity(name string) *entityKind {
	for i := range entityKinds {
		if entityKinds[i].Name == name {
			return &entityKinds[i]
		}
	}
	return nil
}

func entityNames() []string {
	names := make([]string, 0, len(entityKinds))
	for _, kind := range entityKinds {
		names = append(names, kind.Name)
	}
	sort.Strings(names)
	return names
}

func parseIDs(ids []string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(ids))
	for _, raw := range ids {
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("%q is not a uuid", raw)
		}
		out = append(out, id)
	}
	return out, nil
}

// ---- oauth_connection ----

var oauthConnectionKind = entityKind{
	Name:              EntityOAuthConnection,
	SupportsProviders: true,
	List: func(ctx context.Context, db *store.Store) ([]AutomationEntity, error) {
		rows, err := store.NewOAuthConnectionRepository(db.DB()).List(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]AutomationEntity, 0, len(rows))
		for _, row := range rows {
			out = append(out, oauthEntity(row))
		}
		return out, nil
	},
	Resolve: func(ctx context.Context, db *store.Store, ids []string) ([]AutomationEntity, error) {
		parsed, err := parseIDs(ids)
		if err != nil || len(parsed) == 0 {
			return nil, err
		}
		var rows []store.OAuthConnection
		if err := db.DB().WithContext(ctx).Where("id IN ?", parsed).Find(&rows).Error; err != nil {
			return nil, err
		}
		out := make([]AutomationEntity, 0, len(rows))
		for _, row := range rows {
			out = append(out, oauthEntity(row))
		}
		return out, nil
	},
	Check: func(entity AutomationEntity, input AutomationInputSpec) (string, string) {
		provider, _ := entity.Fields["provider"].(string)
		if len(input.Providers) > 0 && !contains(input.Providers, provider) {
			return ReasonProviderNotSupported, fmt.Sprintf("this plugin works with %s accounts, not %s", strings.Join(input.Providers, ", "), provider)
		}
		return "", ""
	},
	Describe: func(entity AutomationEntity) string {
		provider, _ := entity.Fields["provider"].(string)
		return provider
	},
	Snapshot: func(ctx context.Context, db *store.Store, id string) (map[string]any, error) {
		parsed, err := uuid.Parse(id)
		if err != nil {
			return nil, err
		}
		row, err := store.NewOAuthConnectionRepository(db.DB()).GetByID(ctx, parsed)
		if err != nil {
			return nil, err
		}
		current := map[string]any{"now": time.Now().UnixMilli(), "status": row.Status}
		if quota, ok := jsonObject(row.Metadata)["quota"]; ok {
			current["quota"] = quota
		}
		if row.LastSyncAt.Valid {
			current["last_sync_at"] = row.LastSyncAt.Time.UTC().Format(time.RFC3339)
		}
		return current, nil
	},
}

func oauthEntity(row store.OAuthConnection) AutomationEntity {
	name := row.Email
	if name == "" {
		name = row.AccountID
	}
	if name == "" {
		name = row.ID.String()
	}
	return AutomationEntity{
		Type: EntityOAuthConnection, ID: row.ID.String(), Name: name,
		Fields: map[string]any{"provider": row.Provider, "status": row.Status},
	}
}

// ---- api_key ----

var apiKeyKind = entityKind{
	Name:         EntityAPIKey,
	Requirements: []string{EntityRequireFiniteTotalQuota},
	List: func(ctx context.Context, db *store.Store) ([]AutomationEntity, error) {
		rows, err := store.NewAPIKeyRepository(db.DB()).List(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]AutomationEntity, 0, len(rows))
		for _, row := range rows {
			if row.KeyKind == store.APIKeyKindAgentInternal {
				continue
			}
			out = append(out, apiKeyEntity(row))
		}
		return out, nil
	},
	Resolve: resolveAPIKeys,
	Check: func(entity AutomationEntity, input AutomationInputSpec) (string, string) {
		if input.Requires == EntityRequireFiniteTotalQuota {
			if _, finite := entity.Fields["totalLimit"]; !finite {
				return ReasonNoFiniteTotalQuota, "no finite total quota, so there is nothing to reset"
			}
		}
		return "", ""
	},
	Describe: func(entity AutomationEntity) string {
		used, _ := asFloat(entity.Fields["totalUsed"])
		if limit, ok := asFloat(entity.Fields["totalLimit"]); ok {
			return fmt.Sprintf("%.2f / %.2f", used, limit)
		}
		return ""
	},
	Snapshot: func(ctx context.Context, db *store.Store, id string) (map[string]any, error) {
		found, err := resolveAPIKeys(ctx, db, []string{id})
		if err != nil || len(found) == 0 {
			return nil, fmt.Errorf("api key %s not found", id)
		}
		snapshot := map[string]any{"now": time.Now().UnixMilli()}
		for key, value := range found[0].Fields {
			snapshot[key] = value
		}
		return snapshot, nil
	},
}

func resolveAPIKeys(ctx context.Context, db *store.Store, ids []string) ([]AutomationEntity, error) {
	parsed, err := parseIDs(ids)
	if err != nil || len(parsed) == 0 {
		return nil, err
	}
	rows, err := store.NewAPIKeyRepository(db.DB()).ListByIDs(ctx, parsed)
	if err != nil {
		return nil, err
	}
	out := make([]AutomationEntity, 0, len(rows))
	for _, row := range rows {
		out = append(out, apiKeyEntity(row))
	}
	return out, nil
}

func apiKeyEntity(row store.APIKey) AutomationEntity {
	fields := map[string]any{
		"totalUsed":  row.QuotaTotalUsed,
		"dailyUsed":  row.QuotaDailyUsed,
		"weeklyUsed": row.QuotaWeeklyUsed,
	}
	if finiteTotalQuota(row) {
		fields["totalLimit"] = row.QuotaLimit.Float64
	}
	return AutomationEntity{Type: EntityAPIKey, ID: row.ID.String(), Name: row.Name, Fields: fields}
}
