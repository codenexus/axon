// Package inventory collects host-level metrics for the heartbeat payload.
package inventory

import (
	"runtime"

	"github.com/codenexus/axon/pulse/internal/protocol"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
)

// Collect gathers current host metrics. Best-effort: a failed sub-probe
// (e.g. a disk partition that can't be statted) is skipped rather than
// failing the whole heartbeat.
func Collect() protocol.HostMetrics {
	m := protocol.HostMetrics{
		CPUCores: runtime.NumCPU(),
		OS:       runtime.GOOS,
		Platform: runtime.GOARCH,
	}

	if pct, err := cpu.Percent(0, false); err == nil && len(pct) > 0 {
		m.CPUUsagePercent = pct[0]
	}

	if vm, err := mem.VirtualMemory(); err == nil {
		m.RAMTotalBytes = vm.Total
		m.RAMUsedBytes = vm.Used
	}

	if uptime, err := host.Uptime(); err == nil {
		m.UptimeSeconds = uptime
	}

	if parts, err := disk.Partitions(false); err == nil {
		for _, p := range parts {
			usage, err := disk.Usage(p.Mountpoint)
			if err != nil {
				continue
			}
			m.Disks = append(m.Disks, protocol.DiskUsage{
				Mount:      p.Mountpoint,
				TotalBytes: usage.Total,
				UsedBytes:  usage.Used,
			})
		}
	}

	return m
}
