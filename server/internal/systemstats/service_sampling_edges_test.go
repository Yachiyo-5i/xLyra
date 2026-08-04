package systemstats

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestSampleNetworkSkipsRatesWhenTimestampDoesNotAdvance(t *testing.T) {
	t.Parallel()

	service := NewService()
	service.lastNet = netCounters{rx: math.MaxUint64, tx: math.MaxUint64}
	service.lastNetAt = time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)

	stats := service.sampleNetwork(context.Background(), service.lastNetAt)

	if stats.RxBytesPerSec != 0 || stats.TxBytesPerSec != 0 {
		t.Fatalf("network rates = (%v, %v), want zeros", stats.RxBytesPerSec, stats.TxBytesPerSec)
	}
}

func TestSampleDiskSkipsRatesWhenTimestampDoesNotAdvance(t *testing.T) {
	t.Parallel()

	service := NewService()
	service.lastDisk = diskCounters{read: math.MaxUint64, write: math.MaxUint64}
	service.lastDiskAt = time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)

	stats := service.sampleDisk(context.Background(), service.lastDiskAt, defaultDiskPath)

	if stats.ReadBytesPerSec != 0 || stats.WriteBytesPerSec != 0 {
		t.Fatalf("disk rates = (%v, %v), want zeros", stats.ReadBytesPerSec, stats.WriteBytesPerSec)
	}
}
