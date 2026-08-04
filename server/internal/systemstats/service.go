package systemstats

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	gopsnet "github.com/shirou/gopsutil/v4/net"

	"xlyra/server/internal/config"
)

const defaultDiskPath = "/"

type Service struct {
	mu         sync.Mutex
	lastNet    netCounters
	lastNetAt  time.Time
	lastDisk   diskCounters
	lastDiskAt time.Time
	timeZone   config.TimeZone
}

func NewService(timeZones ...config.TimeZone) *Service {
	return &Service{timeZone: serviceTimeZone(timeZones...)}
}

type Snapshot struct {
	Timestamp            time.Time    `json:"timestamp"`
	Time                 string       `json:"time"`
	CPUUsagePercent      float64      `json:"cpu_usage_percent"`
	MemoryUsagePercent   float64      `json:"memory_usage_percent"`
	DiskUsagePercent     float64      `json:"disk_usage_percent"`
	DiskReadBytesPerSec  float64      `json:"disk_read_bytes_per_sec"`
	DiskWriteBytesPerSec float64      `json:"disk_write_bytes_per_sec"`
	DiskReadBytesTotal   uint64       `json:"disk_read_bytes_total"`
	DiskWriteBytesTotal  uint64       `json:"disk_write_bytes_total"`
	NetworkRxBytesPerSec float64      `json:"network_rx_bytes_per_sec"`
	NetworkTxBytesPerSec float64      `json:"network_tx_bytes_per_sec"`
	NetworkRxBytesTotal  uint64       `json:"network_rx_bytes_total"`
	NetworkTxBytesTotal  uint64       `json:"network_tx_bytes_total"`
	CPU                  CPUStats     `json:"cpu"`
	Memory               MemoryStats  `json:"memory"`
	Disk                 DiskStats    `json:"disk"`
	Network              NetworkStats `json:"network"`
}

type CPUStats struct {
	UsagePercent float64 `json:"usage_percent"`
	Cores        int     `json:"cores"`
	Load1        float64 `json:"load1"`
	Load5        float64 `json:"load5"`
	Load15       float64 `json:"load15"`
}

type MemoryStats struct {
	TotalBytes     uint64  `json:"total_bytes"`
	UsedBytes      uint64  `json:"used_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	UsagePercent   float64 `json:"usage_percent"`
}

type DiskStats struct {
	Path             string  `json:"path"`
	TotalBytes       uint64  `json:"total_bytes"`
	UsedBytes        uint64  `json:"used_bytes"`
	FreeBytes        uint64  `json:"free_bytes"`
	UsagePercent     float64 `json:"usage_percent"`
	ReadBytesPerSec  float64 `json:"read_bytes_per_sec"`
	WriteBytesPerSec float64 `json:"write_bytes_per_sec"`
	ReadBytesTotal   uint64  `json:"read_bytes_total"`
	WriteBytesTotal  uint64  `json:"write_bytes_total"`
}

type NetworkStats struct {
	RxBytesPerSec float64 `json:"rx_bytes_per_sec"`
	TxBytesPerSec float64 `json:"tx_bytes_per_sec"`
	RxBytesTotal  uint64  `json:"rx_bytes_total"`
	TxBytesTotal  uint64  `json:"tx_bytes_total"`
}

type netCounters struct {
	rx uint64
	tx uint64
}

type diskCounters struct {
	read  uint64
	write uint64
}

func (s *Service) Sample(ctx context.Context, now time.Time) Snapshot {
	cpuStats := sampleCPU(ctx)
	memoryStats := sampleMemory(ctx)
	diskStats := s.sampleDisk(ctx, now, defaultDiskPath)
	networkStats := s.sampleNetwork(ctx, now)

	return Snapshot{
		Timestamp:            now,
		Time:                 s.timeZone.Format(now, "15:04:05"),
		CPUUsagePercent:      cpuStats.UsagePercent,
		MemoryUsagePercent:   memoryStats.UsagePercent,
		DiskUsagePercent:     diskStats.UsagePercent,
		DiskReadBytesPerSec:  diskStats.ReadBytesPerSec,
		DiskWriteBytesPerSec: diskStats.WriteBytesPerSec,
		DiskReadBytesTotal:   diskStats.ReadBytesTotal,
		DiskWriteBytesTotal:  diskStats.WriteBytesTotal,
		NetworkRxBytesPerSec: networkStats.RxBytesPerSec,
		NetworkTxBytesPerSec: networkStats.TxBytesPerSec,
		NetworkRxBytesTotal:  networkStats.RxBytesTotal,
		NetworkTxBytesTotal:  networkStats.TxBytesTotal,
		CPU:                  cpuStats,
		Memory:               memoryStats,
		Disk:                 diskStats,
		Network:              networkStats,
	}
}

func serviceTimeZone(timeZones ...config.TimeZone) config.TimeZone {
	return config.TimeZoneOrDefault(timeZones...)
}

func sampleCPU(ctx context.Context) CPUStats {
	var usage float64
	if values, err := cpu.PercentWithContext(ctx, 0, false); err == nil && len(values) > 0 {
		usage = cleanFloat(values[0])
	}

	cores, _ := cpu.CountsWithContext(ctx, true)
	loadAvg, _ := load.AvgWithContext(ctx)
	stats := CPUStats{
		UsagePercent: usage,
		Cores:        cores,
	}
	if loadAvg != nil {
		stats.Load1 = cleanFloat(loadAvg.Load1)
		stats.Load5 = cleanFloat(loadAvg.Load5)
		stats.Load15 = cleanFloat(loadAvg.Load15)
	}
	return stats
}

func sampleMemory(ctx context.Context) MemoryStats {
	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil || vm == nil {
		return MemoryStats{}
	}
	return MemoryStats{
		TotalBytes:     vm.Total,
		UsedBytes:      vm.Used,
		AvailableBytes: vm.Available,
		UsagePercent:   cleanFloat(vm.UsedPercent),
	}
}

func (s *Service) sampleDisk(ctx context.Context, now time.Time, path string) DiskStats {
	usage, err := disk.UsageWithContext(ctx, path)
	stats := DiskStats{Path: path}
	if err == nil && usage != nil {
		stats.TotalBytes = usage.Total
		stats.UsedBytes = usage.Used
		stats.FreeBytes = usage.Free
		stats.UsagePercent = cleanFloat(usage.UsedPercent)
	}

	current := aggregateDiskCounters(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()

	stats.ReadBytesTotal = current.read
	stats.WriteBytesTotal = current.write
	if !s.lastDiskAt.IsZero() && now.After(s.lastDiskAt) {
		elapsed := now.Sub(s.lastDiskAt).Seconds()
		stats.ReadBytesPerSec = bytesRate(current.read, s.lastDisk.read, elapsed)
		stats.WriteBytesPerSec = bytesRate(current.write, s.lastDisk.write, elapsed)
	}

	s.lastDisk = current
	s.lastDiskAt = now
	return stats
}

func aggregateDiskCounters(ctx context.Context) diskCounters {
	counters, err := disk.IOCountersWithContext(ctx)
	if err != nil {
		return diskCounters{}
	}
	var total diskCounters
	for _, item := range counters {
		total.read += item.ReadBytes
		total.write += item.WriteBytes
	}
	return total
}

func (s *Service) sampleNetwork(ctx context.Context, now time.Time) NetworkStats {
	current := aggregateNetworkCounters(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()

	stats := NetworkStats{
		RxBytesTotal: current.rx,
		TxBytesTotal: current.tx,
	}
	if !s.lastNetAt.IsZero() && now.After(s.lastNetAt) {
		elapsed := now.Sub(s.lastNetAt).Seconds()
		stats.RxBytesPerSec = bytesRate(current.rx, s.lastNet.rx, elapsed)
		stats.TxBytesPerSec = bytesRate(current.tx, s.lastNet.tx, elapsed)
	}

	s.lastNet = current
	s.lastNetAt = now
	return stats
}

func aggregateNetworkCounters(ctx context.Context) netCounters {
	counters, err := gopsnet.IOCountersWithContext(ctx, false)
	if err != nil || len(counters) == 0 {
		return netCounters{}
	}
	return netCounters{rx: counters[0].BytesRecv, tx: counters[0].BytesSent}
}

func bytesRate(current uint64, previous uint64, seconds float64) float64 {
	if seconds <= 0 || current < previous {
		return 0
	}
	return cleanFloat(float64(current-previous) / seconds)
}

func cleanFloat(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0
	}
	return value
}
