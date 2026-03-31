//go:build linux

package collector

import (
	"os"
	"strings"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/load"
	"go.uber.org/zap"
	pb "github.com/dowork-shanqiu/xuannexus-agent/api/proto/agent"
)

// CollectPlatformSpecificStaticInfo collects Linux-specific static information
func (c *SystemCollector) CollectPlatformSpecificStaticInfo(info *pb.StaticInfo) {
	// Read /etc/os-release for more detailed OS information
	if content, err := os.ReadFile("/etc/os-release"); err == nil {
		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				info.OsVersion = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
				break
			}
		}
	}

	// Get CPU model from /proc/cpuinfo. The core count is obtained separately
	// using cpu.Counts(true) rather than cpuInfo[0].Cores, because the latter
	// only reports per-socket cores (always 1 for a vCPU on a typical VPS).
	if cpuInfo, err := cpu.Info(); err == nil && len(cpuInfo) > 0 {
		info.CpuModel = cpuInfo[0].ModelName
	}

	if logicalCores, err := cpu.Counts(true); err == nil && logicalCores > 0 {
		info.CpuCores = int32(logicalCores)
	} else if err != nil {
		// cpu.Counts failed; retain the value already set by CollectStaticInfo
		// (runtime.NumCPU() fallback) and log a warning.
		c.logger.Warn("Failed to get logical CPU count, using previous value",
			zap.Error(err),
			zap.Int32("cores", info.CpuCores))
	}

	c.logger.Debug("Linux CPU info collected",
		zap.String("model", info.CpuModel),
		zap.Int32("cores", info.CpuCores))
}

// CollectPlatformSpecificDynamicMetrics collects Linux-specific dynamic metrics
func (c *SystemCollector) CollectPlatformSpecificDynamicMetrics(metrics *pb.DynamicMetrics) {
	// Get load average (available on Linux)
	if loadAvg, err := load.Avg(); err == nil {
		metrics.LoadAverage_1M = loadAvg.Load1
		metrics.LoadAverage_5M = loadAvg.Load5
		metrics.LoadAverage_15M = loadAvg.Load15

		c.logger.Debug("Linux load average collected",
			zap.Float64("load1", loadAvg.Load1),
			zap.Float64("load5", loadAvg.Load5),
			zap.Float64("load15", loadAvg.Load15))
	} else {
		c.logger.Warn("Failed to get load average", zap.Error(err))
	}

	// Filter disk partitions - skip virtual filesystems common on Linux
	if partitions, err := disk.Partitions(false); err == nil {
		// Clear existing disk usage to replace with filtered version
		metrics.DiskUsage = nil

		for _, partition := range partitions {
			// Skip common Linux virtual filesystems
			if isVirtualFilesystem(partition.Fstype) {
				continue
			}

			// Skip tmpfs and other memory-based filesystems
			if partition.Fstype == "tmpfs" || partition.Fstype == "devtmpfs" {
				continue
			}

			if usage, err := disk.Usage(partition.Mountpoint); err == nil {
				metrics.DiskUsage = append(metrics.DiskUsage, &pb.DiskUsage{
					MountPoint:   partition.Mountpoint,
					Filesystem:   partition.Fstype,
					Total:        int64(usage.Total),
					Used:         int64(usage.Used),
					Available:    int64(usage.Free),
					UsagePercent: usage.UsedPercent,
				})
			}
		}
	}
}

// isVirtualFilesystem checks if a filesystem type is virtual
func isVirtualFilesystem(fstype string) bool {
	virtualFS := []string{
		"proc", "sysfs", "devpts", "cgroup", "cgroup2",
		"debugfs", "securityfs", "tracefs", "binfmt_misc",
		"configfs", "fusectl", "pstore", "autofs",
	}

	for _, vfs := range virtualFS {
		if fstype == vfs {
			return true
		}
	}
	return false
}
