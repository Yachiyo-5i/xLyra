package site

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

func TestCodexModelRebuildPersistsImageProtocols(t *testing.T) {
	for _, test := range []struct {
		name            string
		raw             string
		latestOverride  string
		wantCredentials int
	}{
		{"declared", `{"supported_endpoint_types":["openai-image"]}`, "", 1},
		{"nested", `{"raw":{"supported_endpoint_types":["openai-image"]}}`, "", 1},
		{"missing", `{}`, "", 1},
		{"empty", `{"supported_endpoint_types":[]}`, "", 1},
		{"empty masking nested", `{"supported_endpoint_types":[],"raw":{"supported_endpoint_types":["openai-image"]}}`, "", 1},
		{"disabled", `{"supported_endpoint_types":[],"endpoint_override":{"mode":"disabled"}}`, "", 0},
		{"empty allowlist", `{"supported_endpoint_types":[],"endpoint_override":{"mode":"allowlist","endpoint_types":[]}}`, "", 0},
		{"image allowlist", `{"supported_endpoint_types":[],"endpoint_override":{"mode":"allowlist","endpoint_types":["openai-image"]}}`, "", 1},
		{"text allowlist", `{"supported_endpoint_types":[],"endpoint_override":{"mode":"allowlist","endpoint_types":["openai-response"]}}`, "", 0},
		{"concurrent override", `{"supported_endpoint_types":[]}`, `{"mode":"disabled"}`, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			siteID, credentialID, modelID := uuid.New(), uuid.New(), uuid.New()
			meta := siteMustJSONMap(t, []byte(test.raw))
			meta["source"] = "codex_image_route"
			meta["manual"] = true
			keyModel := store.SiteAPIKeyModel{ID: uuid.New(), SiteID: siteID, SiteCredentialID: credentialID, UpstreamModelName: "gpt-image-2", Available: true, Enabled: true, Raw: siteJSONMeta(t, meta)}
			var saved store.SiteModel
			bound, locked := false, false
			var writeTx gorm.ConnPool
			db := siteTransactionPostgresGorm(t)
			siteReplaceQueryCallback(t, db, func(tx *gorm.DB) {
				switch dest := tx.Statement.Dest.(type) {
				case *[]store.SiteAPIKeyModel:
					if _, ok := tx.Statement.Clauses["FOR"]; ok {
						locked = true
						if test.latestOverride != "" {
							meta["endpoint_override"] = json.RawMessage(test.latestOverride)
							keyModel.Raw = siteJSONMeta(t, meta)
						}
					}
					*dest = []store.SiteAPIKeyModel{keyModel}
				case *[]store.SiteAPIKeyState:
					*dest = []store.SiteAPIKeyState{{SiteCredentialID: credentialID, Enabled: true}}
				case *[]store.SiteCredential:
					*dest = []store.SiteCredential{{ID: credentialID, SiteID: siteID, CredentialType: "api_key"}}
				case *store.SiteModel:
					*dest = store.SiteModel{ID: modelID, SiteID: siteID, UpstreamName: "gpt-image-2", Status: "active"}
				case *[]store.SiteModel:
					*dest = nil
				case *[]store.CanonicalModel:
					*dest = nil
				case *[]store.Site:
					*dest = nil
				case *[]store.RouteCooldown:
					*dest = nil
				default:
					t.Fatalf("unexpected query: %T", dest)
				}
				tx.RowsAffected = 1
			})
			siteReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
				if model, ok := tx.Statement.Dest.(*store.SiteModel); ok {
					saved = *model
					writeTx = tx.Statement.ConnPool
					if _, ok := writeTx.(gorm.TxCommitter); !ok {
						t.Fatal("site model write is not transactional")
					}
				} else if values, ok := tx.Statement.Dest.(map[string]any); ok {
					if tx.Statement.ConnPool != writeTx {
						t.Fatal("credential write is outside the site model transaction")
					}
					if raw, ok := values["raw"].(store.JSON); ok {
						keyModel.Raw = raw
					} else if values["site_model_id"] == modelID {
						keyModel.SiteModelID = uuid.NullUUID{UUID: modelID, Valid: true}
						bound = true
					}
				}
				tx.RowsAffected = 1
			})
			service := NewService(siteStoreWithGorm(t, db), siteTestMasterKey)
			if _, err := service.syncSiteModelsFromAPIKeyState(t.Context(), siteID, "codex", ""); err != nil {
				t.Fatal(err)
			}
			capabilities := siteMustJSONMap(t, saved.Capabilities)
			if !reflect.DeepEqual(capabilities["supported_endpoint_types"], []any{"openai-image"}) {
				t.Fatalf("persisted capabilities = %s, want image protocol", saved.Capabilities)
			}
			if capabilities["source"] != "codex_image_route" || !bound || !locked {
				t.Fatalf("source=%v bound=%t locked=%t", capabilities["source"], bound, locked)
			}
			persistedRaw := siteMustJSONMap(t, keyModel.Raw)
			for _, field := range []string{"source", "manual", "raw", "endpoint_override"} {
				want := meta[field]
				if field == "endpoint_override" && test.latestOverride != "" {
					want = siteMustJSONMap(t, []byte(test.latestOverride))
				}
				if !reflect.DeepEqual(persistedRaw[field], want) {
					t.Fatalf("credential field %s changed: got %v, want %v", field, persistedRaw[field], want)
				}
			}
			policy := store.ResolveSiteModelEndpointPolicy(store.CanonicalModel{}, saved, store.SiteModelEndpointOverride{})
			if !reflect.DeepEqual(policy.SupportedEndpointTypes, []string{"openai-image"}) {
				t.Fatalf("effective protocols without catalog declaration = %v", policy.SupportedEndpointTypes)
			}
			if got := keyModel.Capabilities().SupportedEndpointTypes; !reflect.DeepEqual(got, []string{"openai-image"}) {
				t.Fatalf("persisted credential protocols = %v", got)
			}
			credentials, err := store.NewGatewayRepository(db).ListCredentialsForSiteModelWithEndpointTypes(t.Context(), siteID, modelID, policy.SupportedEndpointTypes)
			if err != nil {
				t.Fatal(err)
			}
			if len(credentials) != test.wantCredentials {
				t.Fatalf("gateway credentials = %d, want %d", len(credentials), test.wantCredentials)
			}
		})
	}
}

func TestCodexModelRebuildPreservesDeclaredTextProtocols(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want []string
	}{
		{`{"supported_endpoint_types":["openai-response"]}`, []string{"openai-response"}},
		{`{"raw":{"supported_endpoint_types":["openai","openai-response"]}}`, []string{"openai", "openai-response"}},
		{`{}`, []string{"openai", "openai-response"}},
	} {
		encoded := codexSiteModelCapabilities(store.JSON(test.raw), "gpt-5-codex")
		capabilities := (store.SiteAPIKeyModel{Raw: encoded}).Capabilities()
		if !reflect.DeepEqual(capabilities.SupportedEndpointTypes, test.want) {
			t.Fatalf("raw=%s protocols=%v, want %v", test.raw, capabilities.SupportedEndpointTypes, test.want)
		}
	}
}
