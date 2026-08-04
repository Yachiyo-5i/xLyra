package systemstats

import (
	"context"
	"math"
	"testing"
	"time"

	"xlyra/server/internal/config"
)

func TestBytesRate(t *testing.T) {
	t.Parallel()

	if got := bytesRate(1500, 500, 2); got != 500 {
		t.Fatalf("bytesRate = %v, want 500", got)
	}
	if got := bytesRate(500, 1500, 2); got != 0 {
		t.Fatalf("bytesRate with counter reset = %v, want 0", got)
	}
	if got := bytesRate(1500, 500, 0); got != 0 {
		t.Fatalf("bytesRate with zero duration = %v, want 0", got)
	}
}

func TestCleanFloat(t *testing.T) {
	t.Parallel()

	if got := cleanFloat(-1); got != 0 {
		t.Fatalf("cleanFloat negative = %v, want 0", got)
	}
	if got := cleanFloat(12.5); got != 12.5 {
		t.Fatalf("cleanFloat normal = %v, want 12.5", got)
	}
}

func TestCleanFloatRejectsNonFiniteValues(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		value float64
	}{
		{name: "nan", value: math.NaN()},
		{name: "positive infinity", value: math.Inf(1)},
		{name: "negative infinity", value: math.Inf(-1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := cleanFloat(tc.value); got != 0 {
				t.Fatalf("cleanFloat(%v) = %v, want 0", tc.value, got)
			}
		})
	}
}

func TestNewServiceFallsBackToResolvedTimeZone(t *testing.T) {
	t.Setenv("TZ", "UTC")
	t.Setenv("TimeZone", "")

	service := NewService(config.TimeZone{})

	if service.timeZone.Name != "UTC" {
		t.Fatalf("time zone = %q, want UTC", service.timeZone.Name)
	}
}

func TestSampleFormatsTimeInConfiguredTimeZone(t *testing.T) {
	t.Parallel()

	service := NewService(config.LoadTimeZone("UTC"))
	snapshot := service.Sample(context.Background(), time.Date(2026, 5, 10, 3, 4, 5, 0, time.FixedZone("CST", 8*60*60)))

	if snapshot.Time != "19:04:05" {
		t.Fatalf("expected UTC sample time, got %q", snapshot.Time)
	}
}

func TestSampleMirrorsNestedStatsInTopLevelFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 10, 3, 4, 5, 0, time.UTC)
	service := NewService(config.LoadTimeZone("UTC"))

	snapshot := service.Sample(context.Background(), now)

	if !snapshot.Timestamp.Equal(now) {
		t.Fatalf("timestamp = %s, want %s", snapshot.Timestamp, now)
	}
	if snapshot.Time != "03:04:05" {
		t.Fatalf("time = %q, want 03:04:05", snapshot.Time)
	}
	if snapshot.CPUUsagePercent != snapshot.CPU.UsagePercent {
		t.Fatalf("cpu usage top-level = %v, nested = %v", snapshot.CPUUsagePercent, snapshot.CPU.UsagePercent)
	}
	if snapshot.MemoryUsagePercent != snapshot.Memory.UsagePercent {
		t.Fatalf("memory usage top-level = %v, nested = %v", snapshot.MemoryUsagePercent, snapshot.Memory.UsagePercent)
	}
	if snapshot.DiskUsagePercent != snapshot.Disk.UsagePercent {
		t.Fatalf("disk usage top-level = %v, nested = %v", snapshot.DiskUsagePercent, snapshot.Disk.UsagePercent)
	}
	if snapshot.DiskReadBytesPerSec != snapshot.Disk.ReadBytesPerSec || snapshot.DiskWriteBytesPerSec != snapshot.Disk.WriteBytesPerSec {
		t.Fatalf("disk rates top-level = (%v, %v), nested = (%v, %v)", snapshot.DiskReadBytesPerSec, snapshot.DiskWriteBytesPerSec, snapshot.Disk.ReadBytesPerSec, snapshot.Disk.WriteBytesPerSec)
	}
	if snapshot.DiskReadBytesTotal != snapshot.Disk.ReadBytesTotal || snapshot.DiskWriteBytesTotal != snapshot.Disk.WriteBytesTotal {
		t.Fatalf("disk totals top-level = (%d, %d), nested = (%d, %d)", snapshot.DiskReadBytesTotal, snapshot.DiskWriteBytesTotal, snapshot.Disk.ReadBytesTotal, snapshot.Disk.WriteBytesTotal)
	}
	if snapshot.NetworkRxBytesPerSec != snapshot.Network.RxBytesPerSec || snapshot.NetworkTxBytesPerSec != snapshot.Network.TxBytesPerSec {
		t.Fatalf("network rates top-level = (%v, %v), nested = (%v, %v)", snapshot.NetworkRxBytesPerSec, snapshot.NetworkTxBytesPerSec, snapshot.Network.RxBytesPerSec, snapshot.Network.TxBytesPerSec)
	}
	if snapshot.NetworkRxBytesTotal != snapshot.Network.RxBytesTotal || snapshot.NetworkTxBytesTotal != snapshot.Network.TxBytesTotal {
		t.Fatalf("network totals top-level = (%d, %d), nested = (%d, %d)", snapshot.NetworkRxBytesTotal, snapshot.NetworkTxBytesTotal, snapshot.Network.RxBytesTotal, snapshot.Network.TxBytesTotal)
	}
	if snapshot.Disk.Path != defaultDiskPath {
		t.Fatalf("disk path = %q, want %q", snapshot.Disk.Path, defaultDiskPath)
	}
}
