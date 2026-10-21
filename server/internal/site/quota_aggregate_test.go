package site

import (
	"math"
	"testing"
)

func TestAggregateQuota(t *testing.T) {
	t.Parallel()
	nan := math.NaN()
	cases := []struct {
		name      string
		mode      string
		items     []QuotaProbeEntry
		ok        bool
		remaining *float64
		limit     *float64
		used      *float64
		unit      string
		resetAt   string
	}{
		{name: "empty", mode: "sum"},
		{name: "nil items", mode: "min", items: nil},
		{
			name:  "none",
			mode:  "none",
			items: []QuotaProbeEntry{{Remaining: f64ptr(4), Limit: f64ptr(8), ResetAt: new("2099-01-15T00:00:00Z")}},
		},
		{
			name:  "unknown",
			mode:  "avg",
			items: []QuotaProbeEntry{{Remaining: f64ptr(4)}},
		},
		{
			name: "sum skips missing negative and non-finite",
			mode: "sum",
			items: []QuotaProbeEntry{
				{Remaining: f64ptr(10), Used: f64ptr(2), Unit: "credits", ResetAt: new("2099-06-01T00:00:00Z")},
				{Limit: f64ptr(5), Unit: "credits", ResetAt: new("2099-01-15T00:00:00Z")},
				{Remaining: f64ptr(-3), Limit: f64ptr(nan), Used: f64ptr(math.Inf(1)), ResetAt: new("2000-01-01T00:00:00Z")},
				{ResetAt: new("not-a-time")},
				{ResetAt: new("")},
			},
			ok:        true,
			remaining: f64ptr(10),
			limit:     f64ptr(5),
			used:      f64ptr(2),
			unit:      "credits",
			resetAt:   "2099-01-15T00:00:00Z",
		},
		{
			name: "sum keeps an all-invalid field missing",
			mode: "sum",
			items: []QuotaProbeEntry{
				{Remaining: f64ptr(-1), Limit: f64ptr(nan), Used: f64ptr(math.Inf(-1))},
			},
			ok: true,
		},
		{
			name:      "sum keeps zero",
			mode:      "sum",
			items:     []QuotaProbeEntry{{Remaining: f64ptr(0), Limit: f64ptr(0), Used: f64ptr(0), Unit: "usd"}},
			ok:        true,
			remaining: f64ptr(0),
			limit:     f64ptr(0),
			used:      f64ptr(0),
			unit:      "usd",
		},
		{
			name: "sum drops mixed units and picks the earliest future reset",
			mode: "sum",
			items: []QuotaProbeEntry{
				{Remaining: f64ptr(1), Unit: "credits", ResetAt: new("2099-01-01T18:00:00Z")},
				{Remaining: f64ptr(2), Unit: "usd", ResetAt: new("2099-01-02T00:00:00+08:00")},
				{Remaining: f64ptr(-4), Unit: "cny", ResetAt: new("2000-01-01T00:00:00Z")},
			},
			ok:        true,
			remaining: f64ptr(3),
			resetAt:   "2099-01-02T00:00:00+08:00",
		},
		{
			name: "min keeps the tightest entry",
			mode: "min",
			items: []QuotaProbeEntry{
				{Remaining: f64ptr(3), Limit: f64ptr(9), Used: f64ptr(6), Unit: "credits", ResetAt: new("2099-06-01T00:00:00Z")},
				{Remaining: f64ptr(8), Limit: f64ptr(20), Used: f64ptr(1), Unit: "usd", ResetAt: new("2099-01-15T00:00:00Z")},
				{Remaining: f64ptr(-1), Limit: f64ptr(100), ResetAt: new("2099-01-01T00:00:00Z")},
			},
			ok:        true,
			remaining: f64ptr(3),
			limit:     f64ptr(9),
			used:      f64ptr(6),
			unit:      "credits",
			resetAt:   "2099-06-01T00:00:00Z",
		},
		{
			name: "min tie prefers the earlier future reset",
			mode: "min",
			items: []QuotaProbeEntry{
				{Remaining: f64ptr(4), Limit: f64ptr(10), Unit: "credits", ResetAt: new("2099-06-01T00:00:00Z")},
				{Remaining: f64ptr(4), Limit: f64ptr(7), Used: f64ptr(1), Unit: "credits", ResetAt: new("2099-01-15T00:00:00Z")},
			},
			ok:        true,
			remaining: f64ptr(4),
			limit:     f64ptr(7),
			used:      f64ptr(1),
			unit:      "credits",
			resetAt:   "2099-01-15T00:00:00Z",
		},
		{
			name: "min drops a past reset on the tightest entry",
			mode: "min",
			items: []QuotaProbeEntry{
				{Remaining: f64ptr(1), Limit: f64ptr(2), Used: f64ptr(1), Unit: "credits", ResetAt: new("2000-01-01T00:00:00Z")},
				{Remaining: f64ptr(5), Limit: f64ptr(9), ResetAt: new("2099-01-15T00:00:00Z")},
			},
			ok:        true,
			remaining: f64ptr(1),
			limit:     f64ptr(2),
			used:      f64ptr(1),
			unit:      "credits",
		},
		{
			name: "min drops an invalid limit and used",
			mode: "min",
			items: []QuotaProbeEntry{
				{Remaining: f64ptr(1), Limit: f64ptr(-5), Used: f64ptr(nan), Unit: "credits"},
			},
			ok:        true,
			remaining: f64ptr(1),
			unit:      "credits",
		},
		{
			name:  "min without a finite remaining",
			mode:  "min",
			items: []QuotaProbeEntry{{Remaining: f64ptr(-2), Limit: f64ptr(3)}, {Used: f64ptr(math.Inf(1))}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := AggregateQuota(tc.mode, tc.items)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v (%#v)", ok, tc.ok, got)
			}
			if !tc.ok {
				if got != (QuotaAggregateSummary{}) {
					t.Fatalf("summary = %#v, want zero", got)
				}
				return
			}
			assertQuotaFloat(t, "remaining", got.Remaining, tc.remaining)
			assertQuotaFloat(t, "limit", got.Limit, tc.limit)
			assertQuotaFloat(t, "used", got.Used, tc.used)
			if got.Unit != tc.unit {
				t.Fatalf("unit = %q, want %q", got.Unit, tc.unit)
			}
			gotReset := ""
			if got.ResetAt != nil {
				gotReset = *got.ResetAt
			}
			if gotReset != tc.resetAt {
				t.Fatalf("resetAt = %q, want %q", gotReset, tc.resetAt)
			}
		})
	}
}

func TestExplicitPluginQuotaAggregate(t *testing.T) {
	ClearPluginSiteTypes()
	t.Cleanup(ClearPluginSiteTypes)
	spec := accountPoolSpec()
	if err := SetPluginSiteTypes([]PluginSiteType{
		{
			Descriptor:     SiteTypeDescriptor{SiteType: "acme.example/pool"},
			PluginID:       "acme.example",
			Enabled:        true,
			Hosts:          []string{"api.example.cn"},
			QuotaAggregate: "min",
			Pool:           &spec,
		},
		{
			Descriptor:     SiteTypeDescriptor{SiteType: "acme.example/chat"},
			PluginID:       "acme.example",
			Enabled:        true,
			Hosts:          []string{"api.example.com"},
			QuotaAggregate: " min ",
		},
		{
			Descriptor:     SiteTypeDescriptor{SiteType: "acme.example/bad"},
			PluginID:       "acme.example",
			Enabled:        true,
			Hosts:          []string{"api.example.com"},
			QuotaAggregate: "avg",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if got := explicitPluginQuotaAggregate("acme.example/pool"); got != "" {
		t.Fatalf("pool = %q", got)
	}
	if got := explicitPluginQuotaAggregate("acme.example/chat"); got != "min" {
		t.Fatalf("api key = %q", got)
	}
	if got := explicitPluginQuotaAggregate("acme.example/bad"); got != "" {
		t.Fatalf("avg = %q", got)
	}
	if got := explicitPluginQuotaAggregate("openai"); got != "" {
		t.Fatalf("builtin = %q", got)
	}
	pool, ok := LookupPluginSiteType("acme.example/pool")
	if !ok || pool.QuotaAggregate != "" || pool.Pool == nil || pool.Pool.QuotaAggregate != "sum" {
		t.Fatalf("pool field = %q pool %#v", pool.QuotaAggregate, pool.Pool)
	}
	chat, ok := LookupPluginSiteType("acme.example/chat")
	if !ok || chat.QuotaAggregate != "min" || chat.Pool != nil {
		t.Fatalf("chat = %#v", chat)
	}
}

func assertQuotaFloat(t *testing.T, name string, got, want *float64) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Fatalf("%s = %v, want missing", name, *got)
		}
		return
	}
	if got == nil || *got != *want {
		t.Fatalf("%s = %v, want %v", name, got, *want)
	}
}
