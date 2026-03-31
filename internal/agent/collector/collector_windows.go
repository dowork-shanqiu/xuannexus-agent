//go:build windows

package collector

import (
	"time"

	"github.com/yusufpapurcu/wmi"
	"golang.org/x/sys/windows/registry"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"go.uber.org/zap"
	pb "github.com/dowork-shanqiu/xuannexus-agent/api/proto/agent"
)

// win32OperatingSystem holds the WMI Win32_OperatingSystem query result
type win32OperatingSystem struct {
	LastBootUpTime time.Time
}

// getWindowsBootTime retrieves the actual last boot time via WMI.
// This is more reliable than gopsutil's GetTickCount64-based calculation,
// which does not reset on hibernate/resume cycles.
func getWindowsBootTime() (int64, error) {
	var dst []win32OperatingSystem
	q := wmi.CreateQuery(&dst, "")
	if err := wmi.Query(q, &dst); err != nil {
		return 0, err
	}
	if len(dst) == 0 {
		return 0, nil
	}
	return dst[0].LastBootUpTime.Unix(), nil
}

// CollectPlatformSpecificStaticInfo collects Windows-specific static information
func (c *SystemCollector) CollectPlatformSpecificStaticInfo(info *pb.StaticInfo) {
	// Get Windows version from registry
	if version, err := getWindowsVersion(); err == nil {
		info.OsVersion = version
		c.logger.Debug("Windows version collected", zap.String("version", version))
	}

	// Override boot time with WMI-based value (more reliable than GetTickCount64 on Windows,
	// which does not reset after hibernate/fast-startup resume)
	if bootTime, err := getWindowsBootTime(); err == nil && bootTime > 0 {
		info.BootTime = bootTime
		c.logger.Debug("Windows boot time collected via WMI", zap.Int64("boot_time", bootTime))
	} else {
		if err != nil {
			c.logger.Warn("Failed to get Windows boot time via WMI, reporting as unavailable", zap.Error(err))
		}
		// Set to 0 to indicate unavailable; gopsutil fallback is unreliable on Windows
		// (GetTickCount64 does not reset on hibernate/fast-startup, giving incorrect values)
		info.BootTime = 0
	}

	// Get detailed CPU information
	if cpuInfo, err := cpu.Info(); err == nil && len(cpuInfo) > 0 {
		info.CpuModel = cpuInfo[0].ModelName
		info.CpuCores = cpuInfo[0].Cores

		c.logger.Debug("Windows CPU info collected",
			zap.String("model", info.CpuModel),
			zap.Int32("cores", info.CpuCores))
	}
}

// CollectPlatformSpecificDynamicMetrics collects Windows-specific dynamic metrics
func (c *SystemCollector) CollectPlatformSpecificDynamicMetrics(metrics *pb.DynamicMetrics) {
	// Windows doesn't support load average metrics (Linux/macOS only)
	// These values are set to 0 and should not be displayed in UI for Windows agents
	metrics.LoadAverage_1M = 0
	metrics.LoadAverage_5M = 0
	metrics.LoadAverage_15M = 0

	// Filter disk partitions - Windows style
	if partitions, err := disk.Partitions(false); err == nil {
		for _, partition := range partitions {
			// Skip CD-ROM drives and network drives
			if partition.Fstype == "CDFS" || partition.Fstype == "" {
				continue
			}

			// Only include fixed drives (C:, D:, etc.)
			if len(partition.Mountpoint) >= 2 && partition.Mountpoint[1] == ':' {
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

	c.logger.Debug("Windows metrics collected",
		zap.Int("disk_count", len(metrics.DiskUsage)))
}

// getWindowsVersion reads Windows version from registry
func getWindowsVersion() (string, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()

	// Try to get ProductName
	productName, _, err := k.GetStringValue("ProductName")
	if err != nil {
		return "", err
	}

	// Try to get ReleaseId/DisplayVersion
	releaseId, _, _ := k.GetStringValue("DisplayVersion")
	if releaseId == "" {
		releaseId, _, _ = k.GetStringValue("ReleaseId")
	}

	if releaseId != "" {
		return productName + " " + releaseId, nil
	}
	return productName, nil
}

