package config

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDefaultGeneralConfigMatchesMapRoundTrip(t *testing.T) {
	t.Parallel()

	cfg := DefaultGeneralConfig()
	if cfg.Tasks.SiteRefreshCron != "0 */15 * * *" {
		t.Fatalf("unexpected site refresh cron: %q", cfg.Tasks.SiteRefreshCron)
	}
	if cfg.Tasks.NewAPICheckinCron != "0 9 * * *" {
		t.Fatalf("unexpected newapi checkin cron: %q", cfg.Tasks.NewAPICheckinCron)
	}
	if cfg.IPWhitelist.Enabled {
		t.Fatal("ip whitelist should default to disabled")
	}
	if len(cfg.IPWhitelist.Entries) != 0 {
		t.Fatalf("expected empty whitelist entries, got %#v", cfg.IPWhitelist.Entries)
	}
	if !cfg.Log.CleanupEnabled || cfg.Log.Level != "info" || cfg.Log.RetentionDays != 30 {
		t.Fatalf("unexpected log defaults: %#v", cfg.Log)
	}
	if !cfg.Data.RequestDetailCleanupEnabled || cfg.Data.RequestDetailRetentionDays != 90 {
		t.Fatalf("unexpected data defaults: %#v", cfg.Data)
	}
	if cfg.Security.SessionLifetimeHours != 24 {
		t.Fatalf("unexpected session lifetime: %d", cfg.Security.SessionLifetimeHours)
	}

	roundTrip := GeneralConfigFromRaw(GeneralConfigToMap(cfg))
	if !reflect.DeepEqual(roundTrip, cfg) {
		t.Fatalf("map round trip mismatch:\n got: %#v\nwant: %#v", roundTrip, cfg)
	}
}

func TestReadGeneralConfigFallsBackAndReadsNestedPath(t *testing.T) {
	t.Parallel()

	defaults := DefaultGeneralConfig()
	cases := map[string]*ConfigFile{
		"nil file":          nil,
		"missing path":      {data: map[string]any{}},
		"nil general value": {data: map[string]any{"global": map[string]any{"general": nil}}},
	}
	for name, confFile := range cases {
		got := ReadGeneralConfig(confFile)
		if !reflect.DeepEqual(got, defaults) {
			t.Fatalf("%s: got %#v, want defaults %#v", name, got, defaults)
		}
	}

	raw := GeneralConfigToMap(defaults)
	raw["security"].(map[string]any)["session_lifetime_hours"] = 72
	confFile := &ConfigFile{data: map[string]any{"global": map[string]any{"general": raw}}}

	got := ReadGeneralConfig(confFile)
	if got.Security.SessionLifetimeHours != 72 {
		t.Fatalf("expected nested general config to be read, got %#v", got.Security)
	}
}

func TestGeneralConfigFromRawParsesSupportedTypesAndNormalizes(t *testing.T) {
	t.Parallel()

	cfg := GeneralConfigFromRaw(map[string]any{
		"tasks": map[string]any{
			"site_refresh_cron":   " */15  1-2  * 1,12 0-7 ",
			"newapi_checkin_cron": " 5 9 * * 1 ",
		},
		"ip_whitelist": map[string]any{
			"enabled": true,
			"entries": []any{" 127.0.0.1 ", "", 99, " 10.0.0.0/8 "},
		},
		"log": map[string]any{
			"level":           " WARN ",
			"cleanup_enabled": false,
			"retention_days":  float64(45),
		},
		"data": map[string]any{
			"request_detail_cleanup_enabled": false,
			"request_detail_retention_days":  int64(180),
		},
		"security": map[string]any{
			"session_lifetime_hours": 48,
		},
	})

	if cfg.Tasks.SiteRefreshCron != "*/15  1-2  * 1,12 0-7" {
		t.Fatalf("site refresh cron was not trimmed: %q", cfg.Tasks.SiteRefreshCron)
	}
	if cfg.Tasks.NewAPICheckinCron != "5 9 * * 1" {
		t.Fatalf("newapi cron was not trimmed: %q", cfg.Tasks.NewAPICheckinCron)
	}
	if !cfg.IPWhitelist.Enabled {
		t.Fatal("expected ip whitelist to be enabled")
	}
	if want := []string{"127.0.0.1", "10.0.0.0/8"}; !reflect.DeepEqual(cfg.IPWhitelist.Entries, want) {
		t.Fatalf("unexpected whitelist entries: got %#v, want %#v", cfg.IPWhitelist.Entries, want)
	}
	if cfg.Log.Level != "warn" || cfg.Log.CleanupEnabled || cfg.Log.RetentionDays != 45 {
		t.Fatalf("unexpected log config: %#v", cfg.Log)
	}
	if cfg.Data.RequestDetailCleanupEnabled || cfg.Data.RequestDetailRetentionDays != 180 {
		t.Fatalf("unexpected data config: %#v", cfg.Data)
	}
	if cfg.Security.SessionLifetimeHours != 48 {
		t.Fatalf("unexpected security config: %#v", cfg.Security)
	}
}

func TestGeneralConfigFromRawIgnoresUnsupportedTypes(t *testing.T) {
	t.Parallel()

	defaults := DefaultGeneralConfig()
	if got := GeneralConfigFromRaw("not a map"); !reflect.DeepEqual(got, defaults) {
		t.Fatalf("non-map raw config should return defaults, got %#v", got)
	}

	cfg := GeneralConfigFromRaw(map[string]any{
		"tasks": "invalid",
		"ip_whitelist": map[string]any{
			"enabled": "true",
			"entries": "127.0.0.1",
		},
		"log": map[string]any{
			"level":           true,
			"cleanup_enabled": "false",
			"retention_days":  12.5,
		},
		"data": map[string]any{
			"request_detail_cleanup_enabled": "false",
			"request_detail_retention_days":  12.5,
		},
		"security": map[string]any{
			"session_lifetime_hours": 12.5,
		},
	})

	if !reflect.DeepEqual(cfg, defaults) {
		t.Fatalf("unsupported raw values should keep defaults, got %#v", cfg)
	}
}

func TestNormalizeGeneralConfigDefaultsAndTrims(t *testing.T) {
	t.Parallel()

	cfg := NormalizeGeneralConfig(GeneralConfig{
		Tasks: GeneralTaskConfig{
			SiteRefreshCron:   " \t ",
			NewAPICheckinCron: "\n",
		},
		IPWhitelist: GeneralIPWhitelistConfig{
			Enabled: true,
			Entries: []string{" 192.168.1.1 ", "", " \t ", "2001:db8::1 "},
		},
		Log: GeneralLogConfig{
			Level:         " \t ",
			RetentionDays: 0,
		},
		Data: GeneralDataConfig{
			RequestDetailRetentionDays: 0,
		},
	})

	defaults := DefaultGeneralConfig()
	if cfg.Tasks.SiteRefreshCron != defaults.Tasks.SiteRefreshCron {
		t.Fatalf("blank site refresh cron should default, got %q", cfg.Tasks.SiteRefreshCron)
	}
	if cfg.Tasks.NewAPICheckinCron != defaults.Tasks.NewAPICheckinCron {
		t.Fatalf("blank newapi cron should default, got %q", cfg.Tasks.NewAPICheckinCron)
	}
	if want := []string{"192.168.1.1", "2001:db8::1"}; !reflect.DeepEqual(cfg.IPWhitelist.Entries, want) {
		t.Fatalf("unexpected normalized whitelist entries: got %#v, want %#v", cfg.IPWhitelist.Entries, want)
	}
	if cfg.Log.Level != defaults.Log.Level || cfg.Log.RetentionDays != defaults.Log.RetentionDays {
		t.Fatalf("log defaults were not applied: %#v", cfg.Log)
	}
	if cfg.Data.RequestDetailRetentionDays != defaults.Data.RequestDetailRetentionDays {
		t.Fatalf("data retention default was not applied: %#v", cfg.Data)
	}
}

func TestValidateGeneralConfigAcceptsBoundaryValues(t *testing.T) {
	t.Parallel()

	cfg := DefaultGeneralConfig()
	cfg.Tasks.SiteRefreshCron = "*/5 0-23 1,15 1-12 0,7"
	cfg.Tasks.NewAPICheckinCron = "0 0 * * *"
	cfg.IPWhitelist.Entries = []string{"127.0.0.1", "::1", "10.0.0.0/8", "2001:db8::/32"}
	cfg.Log.Level = "error"
	cfg.Log.RetentionDays = 1
	cfg.Data.RequestDetailRetentionDays = 1
	cfg.Security.SessionLifetimeHours = 0
	if err := ValidateGeneralConfig(cfg); err != nil {
		t.Fatalf("expected lower boundaries to validate: %v", err)
	}

	cfg.Security.SessionLifetimeHours = 720
	if err := ValidateGeneralConfig(cfg); err != nil {
		t.Fatalf("expected upper session boundary to validate: %v", err)
	}
}

func TestValidateGeneralConfigRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		mutate func(*GeneralConfig)
		want   string
	}{
		"site refresh cron": {
			mutate: func(cfg *GeneralConfig) { cfg.Tasks.SiteRefreshCron = "bad" },
			want:   "tasks.site_refresh_cron",
		},
		"newapi cron": {
			mutate: func(cfg *GeneralConfig) { cfg.Tasks.NewAPICheckinCron = "0 24 * * *" },
			want:   "tasks.newapi_checkin_cron",
		},
		"ip whitelist entry": {
			mutate: func(cfg *GeneralConfig) { cfg.IPWhitelist.Entries = []string{"not-an-ip"} },
			want:   "ip_whitelist.entries",
		},
		"log level": {
			mutate: func(cfg *GeneralConfig) { cfg.Log.Level = "trace" },
			want:   "log.level",
		},
		"log retention": {
			mutate: func(cfg *GeneralConfig) { cfg.Log.RetentionDays = 0 },
			want:   "log.retention_days",
		},
		"request detail retention": {
			mutate: func(cfg *GeneralConfig) { cfg.Data.RequestDetailRetentionDays = -1 },
			want:   "data.request_detail_retention_days",
		},
		"negative session lifetime": {
			mutate: func(cfg *GeneralConfig) { cfg.Security.SessionLifetimeHours = -1 },
			want:   "security.session_lifetime_hours",
		},
		"too large session lifetime": {
			mutate: func(cfg *GeneralConfig) { cfg.Security.SessionLifetimeHours = 721 },
			want:   "security.session_lifetime_hours",
		},
	}

	for name, tc := range cases {
		cfg := DefaultGeneralConfig()
		tc.mutate(&cfg)

		err := ValidateGeneralConfig(cfg)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: expected error containing %q, got %v", name, tc.want, err)
		}
	}
}

func TestValidateCronExpressionCoversSegments(t *testing.T) {
	t.Parallel()

	valid := []string{
		"* * * * *",
		"*/15 0-23 1,15 1-12 0,7",
		"0 9 * * 1-5",
	}
	for _, expr := range valid {
		if err := ValidateCronExpression(expr); err != nil {
			t.Fatalf("expected cron %q to validate: %v", expr, err)
		}
	}

	invalid := []string{
		"",
		"* * * *",
		"*/0 * * * *",
		"*/bad * * * *",
		"10-5 * * * *",
		"0-99 * * * *",
		"1-2-3 * * * *",
		"x * * * *",
		"0 0 0 * *",
		"0 0 * 13 *",
		"0 0 * * 8",
		"0,,1 * * * *",
	}
	for _, expr := range invalid {
		if err := ValidateCronExpression(expr); err == nil {
			t.Fatalf("expected cron %q to be invalid", expr)
		}
	}
}

func TestValidateIPWhitelistEntryCoversAddressAndPrefix(t *testing.T) {
	t.Parallel()

	valid := []string{"192.168.1.10", "::1", "10.0.0.0/8", "2001:db8::/32"}
	for _, entry := range valid {
		if err := ValidateIPWhitelistEntry(entry); err != nil {
			t.Fatalf("expected whitelist entry %q to validate: %v", entry, err)
		}
	}

	invalid := []string{"", " 192.168.1.10 ", "999.0.0.1", "10.0.0.1/33", "2001:db8::/129"}
	for _, entry := range invalid {
		if err := ValidateIPWhitelistEntry(entry); err == nil {
			t.Fatalf("expected whitelist entry %q to be invalid", entry)
		}
	}
}

func TestGeneralMapHelpersHandleSupportedAndFallbackValues(t *testing.T) {
	t.Parallel()

	values := map[string]any{
		"text":       "value",
		"enabled":    true,
		"int":        3,
		"int64":      int64(4),
		"float":      5.0,
		"fractional": 5.5,
		"strings":    []string{"a", "b"},
		"anyStrings": []any{"c", 7, "d"},
	}

	if got := stringFromMap(values, "text", "fallback"); got != "value" {
		t.Fatalf("unexpected string value: %q", got)
	}
	if got := stringFromMap(values, "enabled", "fallback"); got != "fallback" {
		t.Fatalf("non-string should fall back, got %q", got)
	}
	if got := boolFromMap(values, "enabled", false); !got {
		t.Fatal("expected bool helper to read true")
	}
	if got := boolFromMap(values, "text", false); got {
		t.Fatal("non-bool should fall back to false")
	}
	for key, want := range map[string]int{"int": 3, "int64": 4, "float": 5} {
		if got := intFromMap(values, key, 99); got != want {
			t.Fatalf("%s: got %d, want %d", key, got, want)
		}
	}
	if got := intFromMap(values, "fractional", 99); got != 99 {
		t.Fatalf("fractional float should fall back, got %d", got)
	}
	if got := intFromMap(values, "text", 99); got != 99 {
		t.Fatalf("non-number should fall back, got %d", got)
	}
	if got := stringSliceFromMap(values, "strings", nil); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("unexpected []string result: %#v", got)
	}
	if got := stringSliceFromMap(values, "anyStrings", nil); !reflect.DeepEqual(got, []string{"c", "d"}) {
		t.Fatalf("unexpected []any result: %#v", got)
	}
	fallback := []string{"fallback"}
	if got := stringSliceFromMap(values, "text", fallback); !reflect.DeepEqual(got, fallback) {
		t.Fatalf("non-slice should fall back, got %#v", got)
	}
}

func TestValidateCronSegmentBoundaries(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		segment string
		min     int
		max     int
		want    bool
	}{
		"wildcard":        {segment: "*", min: 0, max: 59, want: true},
		"positive step":   {segment: "*/1", min: 0, max: 59, want: true},
		"zero step":       {segment: "*/0", min: 0, max: 59, want: false},
		"bad step":        {segment: "*/x", min: 0, max: 59, want: false},
		"range in bounds": {segment: "0-59", min: 0, max: 59, want: true},
		"reversed range":  {segment: "59-0", min: 0, max: 59, want: false},
		"below range":     {segment: "-1", min: 0, max: 59, want: false},
		"above range":     {segment: "60", min: 0, max: 59, want: false},
		"weekday seven":   {segment: "7", min: 0, max: 7, want: true},
	}

	for name, tc := range cases {
		if got := validateCronSegment(tc.segment, tc.min, tc.max); got != tc.want {
			t.Fatalf("%s: got %v, want %v", name, got, tc.want)
		}
	}
}

func TestTimeZoneZeroValueUsesDefaultBoundary(t *testing.T) {
	t.Parallel()

	var tz TimeZone
	got := tz.StartOfDay(time.Date(2026, 1, 1, 17, 30, 0, 0, time.UTC)).Format(time.RFC3339)
	if got != "2026-01-02T00:00:00+08:00" {
		t.Fatalf("zero-value timezone should use default day boundary, got %s", got)
	}

	trimmed := LoadTimeZone(" UTC ")
	if trimmed.Name != "UTC" {
		t.Fatalf("expected trimmed timezone name, got %q", trimmed.Name)
	}
	if got := trimmed.StartOfHour(time.Date(2026, 1, 1, 17, 30, 0, 0, time.UTC)).Format(time.RFC3339); got != "2026-01-01T17:00:00Z" {
		t.Fatalf("unexpected UTC hour boundary: %s", got)
	}
}
