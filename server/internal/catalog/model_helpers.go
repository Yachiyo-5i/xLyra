package catalog

import (
	"database/sql"
	"strings"
	"xlyra/server/internal/modelcapabilities"
)

func inferEndpointTypes(provider, modelKey, category string) []string {
	if v := modelcapabilities.ModelNameEndpointTypes(modelKey); len(v) > 0 {
		return v
	}
	switch category {
	case "image":
		return []string{"openai-image"}
	case "embedding":
		return []string{"openai"}
	case "audio":
		return []string{"openai"}
	}
	if strings.Contains(strings.ToLower(modelKey), "codex") {
		return []string{"openai-response"}
	}
	if strings.EqualFold(provider, "anthropic") {
		return []string{"anthropic-messages"}
	}
	return []string{"openai"}
}
func extractFloat(m map[string]any, key string) float64 {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	}
	return 0
}
func extractCost(m map[string]any, key string) sql.NullFloat64 {
	v := extractFloat(m, key)
	if v <= 0 {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: v, Valid: true}
}
func extractInt(m map[string]any, key string) sql.NullInt32 {
	if m == nil {
		return sql.NullInt32{}
	}
	raw, ok := m[key]
	if !ok {
		return sql.NullInt32{}
	}
	switch v := raw.(type) {
	case float64:
		return sql.NullInt32{Int32: int32(v), Valid: true}
	case float32:
		return sql.NullInt32{Int32: int32(v), Valid: true}
	case int:
		return sql.NullInt32{Int32: int32(v), Valid: true}
	case int64:
		return sql.NullInt32{Int32: int32(v), Valid: true}
	}
	return sql.NullInt32{}
}
