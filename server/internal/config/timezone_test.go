package config

import (
	"testing"
	"time"
)

func TestResolveTimeZonePrefersTZ(t *testing.T) {
	t.Setenv("TZ", "UTC")
	t.Setenv("TimeZone", "Asia/Tokyo")

	tz := ResolveTimeZone()
	if tz.Name != "UTC" {
		t.Fatalf("expected TZ to win, got %q", tz.Name)
	}
	if got := tz.Format(time.Date(2026, 5, 10, 3, 4, 5, 0, time.UTC), time.RFC3339); got != "2026-05-10T03:04:05Z" {
		t.Fatalf("unexpected formatted time: %s", got)
	}
}

func TestResolveTimeZoneFallsBackToTimeZone(t *testing.T) {
	t.Setenv("TZ", "")
	t.Setenv("TimeZone", "Asia/Tokyo")

	tz := ResolveTimeZone()
	if tz.Name != "Asia/Tokyo" {
		t.Fatalf("expected TimeZone, got %q", tz.Name)
	}
	if got := tz.Format(time.Date(2026, 5, 10, 3, 4, 5, 0, time.UTC), time.RFC3339); got != "2026-05-10T12:04:05+09:00" {
		t.Fatalf("unexpected formatted time: %s", got)
	}
}

func TestLoadTimeZoneFallsBackToDefault(t *testing.T) {
	tz := LoadTimeZone("not-a-zone")
	if tz.Name != DefaultTimeZone {
		t.Fatalf("expected default timezone, got %q", tz.Name)
	}
	if got := tz.StartOfDay(time.Date(2026, 5, 10, 20, 4, 5, 0, time.UTC)).Format(time.RFC3339); got != "2026-05-11T00:00:00+08:00" {
		t.Fatalf("unexpected start of day: %s", got)
	}
}

func TestStartOfHourUsesLocalHourBoundary(t *testing.T) {
	tz := LoadTimeZone("Asia/Kathmandu")

	got := tz.StartOfHour(time.Date(2026, 5, 10, 20, 4, 5, 0, time.UTC)).Format(time.RFC3339)
	if got != "2026-05-11T01:00:00+05:45" {
		t.Fatalf("unexpected start of hour: %s", got)
	}
}

func TestStartOfWeekUsesLocalMondayBoundary(t *testing.T) {
	tz := LoadTimeZone("Asia/Shanghai")

	got := tz.StartOfWeek(time.Date(2026, 7, 19, 20, 0, 0, 0, time.UTC)).Format(time.RFC3339)
	if got != "2026-07-20T00:00:00+08:00" {
		t.Fatalf("unexpected start of week: %s", got)
	}
}
