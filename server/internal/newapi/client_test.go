package newapi

import (
	"context"
	"net/http"
	"testing"
)

func TestClientDetectRecognizesNewAPI(t *testing.T) {
	t.Parallel()

	server := newapiTestServer(t, newapiTestRoutes{
		"/api/status": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, map[string]any{
				"success": true,
				"data": map[string]any{
					"version":                "v0.8.0",
					"quota_per_unit":         500000,
					"quota_display_type":     "quota",
					"checkin_enabled":        true,
					"default_use_auto_group": false,
				},
			})
		},
	})

	result, err := NewClient().Detect(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}

	if !result.Matched {
		t.Fatal("expected NewAPI status to match")
	}
	if result.SiteType != "newapi" {
		t.Fatalf("expected site_type newapi, got %q", result.SiteType)
	}
	if result.Confidence <= 0 {
		t.Fatalf("expected positive confidence, got %f", result.Confidence)
	}
}

func TestClientUserSummaryUsesNewAPIUserAuthHeaders(t *testing.T) {
	t.Parallel()

	server := newapiTestServer(t, newapiTestRoutes{
		"/api/user/self": func(w http.ResponseWriter, r *http.Request) {
			assertUserAuth(t, r)
			writeJSON(t, w, map[string]any{
				"success": true,
				"data": map[string]any{
					"id":         42,
					"quota":      1000,
					"used_quota": 125,
				},
			})
		},
		"/api/token/": func(w http.ResponseWriter, r *http.Request) {
			assertUserAuth(t, r)
			writeJSON(t, w, map[string]any{
				"success": true,
				"data": []map[string]any{
					{"name": "default", "key": "sk-***"},
				},
			})
		},
		"/api/user/models": func(w http.ResponseWriter, r *http.Request) {
			assertUserAuth(t, r)
			writeJSON(t, w, map[string]any{
				"success": true,
				"data":    []string{"gpt-4o-mini", "claude-3-5-sonnet"},
			})
		},
		"/api/pricing": func(w http.ResponseWriter, r *http.Request) {
			assertUserAuth(t, r)
			writeJSON(t, w, map[string]any{
				"success": true,
				"data": map[string]any{
					"gpt-4o-mini": map[string]any{"model_ratio": 0.3},
				},
				"group_ratio": map[string]any{"default": 1},
			})
		},
		"/api/user/checkin": func(w http.ResponseWriter, r *http.Request) {
			assertUserAuth(t, r)
			writeJSON(t, w, map[string]any{
				"success": true,
				"data":    map[string]any{"checked_in": false},
			})
		},
	})

	result, err := NewClient().UserSummary(context.Background(), server.URL, "Bearer access-token", 42)
	if err != nil {
		t.Fatalf("UserSummary returned error: %v", err)
	}

	if !result.CheckinReady {
		t.Fatal("expected checkin to be supported")
	}
	if result.User == nil || result.APIKeys == nil || result.UserModels == nil || result.Pricing == nil {
		t.Fatalf("expected user, api keys, models, and pricing to be populated: %#v", result)
	}
}

func TestClientUserSummaryKeepsUserBalanceWhenOptionalEndpointsFail(t *testing.T) {
	t.Parallel()

	server := newapiTestServer(t, newapiTestRoutes{
		"/api/user/self": func(w http.ResponseWriter, r *http.Request) {
			assertUserAuth(t, r)
			writeJSON(t, w, map[string]any{
				"success": true,
				"data": map[string]any{
					"id":    42,
					"quota": 1000,
				},
			})
		},
		"/api/token/": func(w http.ResponseWriter, r *http.Request) {
			assertUserAuth(t, r)
			writeJSON(t, w, map[string]any{
				"success": true,
				"data": []map[string]any{
					{"id": 7, "name": "masked", "key": "sk-***"},
				},
			})
		},
		"/api/user/models": func(w http.ResponseWriter, r *http.Request) {
			assertUserAuth(t, r)
			writeNewAPIInvalidURL(t, w)
		},
		"/api/pricing": func(w http.ResponseWriter, r *http.Request) {
			assertUserAuth(t, r)
			writeNewAPIInvalidURL(t, w)
		},
		"/api/user/checkin": func(w http.ResponseWriter, r *http.Request) {
			assertUserAuth(t, r)
			writeNewAPIInvalidURL(t, w)
		},
		"/api/token/batch/keys": func(w http.ResponseWriter, r *http.Request) {
			assertUserAuth(t, r)
			writeNewAPIInvalidURL(t, w)
		},
	})

	result, err := NewClient().UserSummary(context.Background(), server.URL, "access-token", 42)
	if err != nil {
		t.Fatalf("UserSummary returned error: %v", err)
	}

	user, _ := result.User.(map[string]any)
	data, _ := user["data"].(map[string]any)
	if data["quota"] == nil {
		t.Fatalf("expected user quota to be preserved: %#v", result.User)
	}
	models, _ := result.UserModels.(map[string]any)
	if success, _ := models["success"].(bool); success {
		t.Fatalf("expected optional models failure payload, got %#v", models)
	}
}

func TestClientAPIKeySummaryUsesGatewayBearerAuth(t *testing.T) {
	t.Parallel()

	server := newapiTestServer(t, newapiTestRoutes{
		"/api/usage/token/": func(w http.ResponseWriter, r *http.Request) {
			assertGatewayAuth(t, r)
			writeJSON(t, w, map[string]any{
				"success": true,
				"data": map[string]any{
					"object":          "token_usage",
					"total_granted":   1000,
					"total_used":      100,
					"total_available": 900,
				},
			})
		},
		"/v1/models": func(w http.ResponseWriter, r *http.Request) {
			assertGatewayAuth(t, r)
			writeJSON(t, w, map[string]any{
				"object": "list",
				"data": []map[string]any{
					{"id": "gpt-4o-mini", "object": "model", "owned_by": "openai"},
				},
			})
		},
	})

	result, err := NewClient().APIKeySummary(context.Background(), server.URL, "sk-test")
	if err != nil {
		t.Fatalf("APIKeySummary returned error: %v", err)
	}

	if result.Usage == nil || result.Models == nil {
		t.Fatalf("expected usage and models to be populated: %#v", result)
	}
}

func TestClientPrimaryAPIKeyUsesUserTokenBatchKeys(t *testing.T) {
	t.Parallel()

	server := newapiTestServer(t, newapiTestRoutes{
		"/api/token/": func(w http.ResponseWriter, r *http.Request) {
			assertUserAuth(t, r)
			writeJSON(t, w, map[string]any{
				"success": true,
				"data": map[string]any{
					"items": []map[string]any{
						{"id": 7, "name": "default", "key": "sk-***"},
					},
				},
			})
		},
		"/api/token/batch/keys": func(w http.ResponseWriter, r *http.Request) {
			assertUserAuth(t, r)
			assertRequestMethod(t, r, http.MethodPost)
			writeJSON(t, w, map[string]any{
				"success": true,
				"data": map[string]any{
					"keys": map[string]any{
						"7": "full-from-newapi",
					},
				},
			})
		},
	})

	key, err := NewClient().PrimaryAPIKey(context.Background(), server.URL, "access-token", 42)
	if err != nil {
		t.Fatalf("PrimaryAPIKey returned error: %v", err)
	}
	if key != "sk-full-from-newapi" {
		t.Fatalf("expected full API key, got %q", key)
	}
}

func TestClientUserAPIKeysFallsBackWhenBatchKeysEndpointIsUnavailable(t *testing.T) {
	t.Parallel()

	server := newapiTestServer(t, newapiTestRoutes{
		"/api/token/": func(w http.ResponseWriter, r *http.Request) {
			assertUserAuth(t, r)
			writeJSON(t, w, map[string]any{
				"success": true,
				"data": map[string]any{
					"items": []map[string]any{
						{"id": 7, "name": "default", "key": "sk-***"},
					},
				},
			})
		},
		"/api/token/batch/keys": func(w http.ResponseWriter, r *http.Request) {
			assertUserAuth(t, r)
			w.WriteHeader(http.StatusNotFound)
			writeJSON(t, w, map[string]any{
				"error": map[string]any{
					"message": "Invalid URL (POST /api/token/batch/keys)",
				},
			})
		},
	})

	keys, err := NewClient().UserAPIKeys(context.Background(), server.URL, "access-token", 42)
	if err != nil {
		t.Fatalf("UserAPIKeys returned error: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 api key, got %d", len(keys))
	}
	if keys[0].Key != "" {
		t.Fatalf("expected masked-only fallback key, got %q", keys[0].Key)
	}
	if keys[0].MaskedKey == "" {
		t.Fatal("expected masked key to be preserved")
	}
}

func TestExtractUserAPIKeysNormalizesRawKeyPrefix(t *testing.T) {
	t.Parallel()

	keys := extractUserAPIKeys(map[string]any{
		"data": map[string]any{
			"items": []any{
				map[string]any{"id": 7, "name": "raw", "key": "raw-from-token-list"},
			},
		},
	})

	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
	if keys[0].Key != "sk-raw-from-token-list" {
		t.Fatalf("expected sk-prefixed raw key, got %q", keys[0].Key)
	}
}

func TestClientUserAPIKeySummariesFetchesEveryTokenKey(t *testing.T) {
	t.Parallel()

	server := newapiTestServer(t, newapiTestRoutes{
		"/api/token/": func(w http.ResponseWriter, r *http.Request) {
			assertUserAuth(t, r)
			writeJSON(t, w, map[string]any{
				"success": true,
				"data": map[string]any{
					"items": []map[string]any{
						{"id": 7, "name": "chat", "key": "sk-***"},
						{"id": 8, "name": "embedding", "key": "sk-***"},
					},
				},
			})
		},
		"/api/token/batch/keys": func(w http.ResponseWriter, r *http.Request) {
			assertUserAuth(t, r)
			writeJSON(t, w, map[string]any{
				"success": true,
				"data": map[string]any{
					"keys": map[string]any{
						"7": "key-chat",
						"8": "key-embedding",
					},
				},
			})
		},
		"/api/usage/token/": func(w http.ResponseWriter, r *http.Request) {
			assertAnyGatewayAuth(t, r)
			writeJSON(t, w, map[string]any{
				"success": true,
				"data": map[string]any{
					"object":          "token_usage",
					"total_available": 100,
				},
			})
		},
		"/v1/models": func(w http.ResponseWriter, r *http.Request) {
			switch r.Header.Get("Authorization") {
			case "Bearer sk-key-chat":
				writeJSON(t, w, map[string]any{
					"object": "list",
					"data": []map[string]any{
						{"id": "kimi-k2.5", "object": "model"},
					},
				})
			case "Bearer sk-key-embedding":
				writeJSON(t, w, map[string]any{
					"object": "list",
					"data": []map[string]any{
						{"id": "Qwen/Qwen3-Embedding-8B", "object": "model"},
					},
				})
			default:
				t.Fatalf("unexpected gateway auth %q", r.Header.Get("Authorization"))
			}
		},
	})

	result, err := NewClient().UserAPIKeySummaries(context.Background(), server.URL, "access-token", 42)
	if err != nil {
		t.Fatalf("UserAPIKeySummaries returned error: %v", err)
	}
	if result.Count != 2 {
		t.Fatalf("expected 2 api keys, got %d", result.Count)
	}
	if got := result.Items[0].ModelIDs[0]; got != "kimi-k2.5" {
		t.Fatalf("expected first key model kimi-k2.5, got %q", got)
	}
	if got := result.Items[1].ModelIDs[0]; got != "Qwen/Qwen3-Embedding-8B" {
		t.Fatalf("expected second key embedding model, got %q", got)
	}
}

func TestClientDoCheckinPostsWithUserAuth(t *testing.T) {
	t.Parallel()

	server := newapiTestServer(t, newapiTestRoutes{
		"/api/user/checkin": func(w http.ResponseWriter, r *http.Request) {
			assertUserAuth(t, r)
			assertRequestMethod(t, r, http.MethodPost)

			writeJSON(t, w, map[string]any{
				"success": true,
				"message": "checked in",
			})
		},
	})

	result, err := NewClient().DoCheckin(context.Background(), server.URL, "access-token", 42)
	if err != nil {
		t.Fatalf("DoCheckin returned error: %v", err)
	}

	if result.Result == nil {
		t.Fatal("expected checkin result to be populated")
	}
}

func TestClientPrimaryAPIKeyReturnsErrorWhenNoFullKeyIsAvailable(t *testing.T) {
	t.Parallel()

	server := newapiTestServer(t, newapiTestRoutes{
		"/api/token/": func(w http.ResponseWriter, r *http.Request) {
			assertUserAuth(t, r)
			writeJSON(t, w, map[string]any{
				"success": true,
				"data": map[string]any{
					"items": []map[string]any{
						{"id": 7, "name": "masked", "key": "sk-***"},
					},
				},
			})
		},
		"/api/token/batch/keys": func(w http.ResponseWriter, r *http.Request) {
			assertUserAuth(t, r)
			writeNewAPIMessage(t, w, http.StatusNotFound, "batch endpoint unavailable")
		},
	})

	_, err := NewClient().PrimaryAPIKey(context.Background(), server.URL, "access-token", 42)
	if err == nil {
		t.Fatal("expected PrimaryAPIKey to fail without a full key")
	}
	if err.Error() != "no api keys returned for newapi user" {
		t.Fatalf("PrimaryAPIKey error = %q", err.Error())
	}
}

func TestExtractBatchTokenKeysNormalizesAndSkipsMalformedEntries(t *testing.T) {
	t.Parallel()

	keys := extractBatchTokenKeys(map[string]any{
		"data": map[string]any{
			"keys": map[string]any{
				"7":      " raw-key ",
				"8":      "sk-already-normalized",
				"bad-id": "sk-ignored",
				"9":      "",
				"10":     123,
			},
		},
	})

	if len(keys) != 2 {
		t.Fatalf("expected 2 normalized keys, got %#v", keys)
	}
	if keys[7] != "sk-raw-key" {
		t.Fatalf("keys[7] = %q, want sk-raw-key", keys[7])
	}
	if keys[8] != "sk-already-normalized" {
		t.Fatalf("keys[8] = %q, want sk-already-normalized", keys[8])
	}
}

func TestUserAPIKeyHelpersNormalizeDeduplicateAndDetectUsableKeys(t *testing.T) {
	t.Parallel()

	keys := []UserAPIKey{
		{ID: 1, Key: " sk-one "},
		{ID: 1, Key: "sk-duplicate-id"},
		{ID: 2, Key: "sk-one"},
		{ID: 0, Key: ""},
		{ID: 0, Key: ""},
	}

	deduped := uniqueUserAPIKeys(keys)
	if len(deduped) != 3 {
		t.Fatalf("expected duplicate id/key entries to be removed, got %#v", deduped)
	}
	if deduped[0].Key != "sk-one" {
		t.Fatalf("expected first key to be trimmed, got %q", deduped[0].Key)
	}
	if !hasUsableAPIKey(deduped) {
		t.Fatal("expected deduped keys to include a usable API key")
	}
	if hasUsableAPIKey([]UserAPIKey{{Key: " \t "}, {MaskedKey: "sk-***"}}) {
		t.Fatal("masked-only keys should not be considered usable")
	}
	if ids := tokenIDs(deduped); len(ids) != 1 || ids[0] != 1 {
		t.Fatalf("tokenIDs = %#v, want [1] based on kept entries with positive IDs only", ids)
	}
}

func TestPayloadMappingHelpersHandleAlternateShapes(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"data": []any{
			map[string]any{"id": float64(7), "name": "seven"},
			"ignored",
			map[string]any{"id": 8, "name": "eight"},
		},
	}

	ids := extractTokenIDs(payload)
	if len(ids) != 2 || ids[0] != 7 || ids[1] != 8 {
		t.Fatalf("extractTokenIDs = %#v, want [7 8]", ids)
	}

	items := tokenItems(payload)
	if len(items) != 2 {
		t.Fatalf("tokenItems returned %#v, want two map items", items)
	}
	if _, ok := numberAsInt("not-a-number"); ok {
		t.Fatal("numberAsInt should reject non-numeric values")
	}
}

func TestUsageAndModelsHelpersMapFallbackPayloads(t *testing.T) {
	t.Parallel()

	modelIDs := modelIDsFromModelsPayload(map[string]any{
		"data": []any{
			map[string]any{"id": "gpt-4o-mini"},
			map[string]any{"id": " "},
			"ignored",
			map[string]any{"id": "claude-sonnet"},
		},
	})
	if len(modelIDs) != 2 || modelIDs[0] != "gpt-4o-mini" || modelIDs[1] != "claude-sonnet" {
		t.Fatalf("modelIDsFromModelsPayload = %#v", modelIDs)
	}

	usage := usageFromTokenRaw(map[string]any{
		"name":                 "default",
		"remain_quota":         float64(300),
		"used_quota":           200,
		"expired_time":         1893456000,
		"unlimited_quota":      false,
		"model_limits":         []any{"gpt-4o-mini"},
		"model_limits_enabled": true,
	})
	data, ok := usage["data"].(map[string]any)
	if !ok {
		t.Fatalf("usage data = %#v, want map", usage["data"])
	}
	if data["total_granted"] != 500 || data["total_used"] != 200 || data["total_available"] != 300 {
		t.Fatalf("usage totals = %#v", data)
	}
	if !hasUsageData(usage) {
		t.Fatal("expected fallback usage payload to count as usage data")
	}
	if hasUsageData(map[string]any{"data": map[string]any{"object": "token_usage"}}) {
		t.Fatal("usage payload without totals should not count as usage data")
	}
}
