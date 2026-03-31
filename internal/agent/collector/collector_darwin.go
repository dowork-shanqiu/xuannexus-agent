//go:build darwin

package collector

import (
	"os/exec"
	"strings"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/load"
	"go.uber.org/zap"
	pb "github.com/dowork-shanqiu/xuannexus-agent/api/proto/agent"
)

// CollectPlatformSpecificStaticInfo collects macOS-specific static information
func (c *SystemCollector) CollectPlatformSpecificStaticInfo(info *pb.StaticInfo) {
	// Get macOS version using sw_vers command
	if version, err := getMacOSVersion(); err == nil {
		info.OsVersion = version
		c.logger.Debug("macOS version collected", zap.String("version", version))
	}

	// Get detailed CPU information
	if cpuInfo, err := cpu.Info(); err == nil && len(cpuInfo) > 0 {
		info.CpuModel = cpuInfo[0].ModelName
		info.CpuCores = cpuInfo[0].Cores

		c.logger.Debug("macOS CPU info collected",
			zap.String("model", info.CpuModel),
			zap.Int32("cores", info.CpuCores))
	}

	// Get more detailed hardware info using sysctl if needed
	if info.CpuModel == "" || info.CpuModel == "Unknown" {
		if model, err := getMacOSCPUModel(); err == nil {
			info.CpuModel = model
		}
	}
}

// CollectPlatformSpecificDynamicMetrics collects macOS-specific dynamic metrics
func (c *SystemCollector) CollectPlatformSpecificDynamicMetrics(metrics *pb.DynamicMetrics) {
	// Get load average (available on macOS)
	if loadAvg, err := load.Avg(); err == nil {
		metrics.LoadAverage_1M = loadAvg.Load1
		metrics.LoadAverage_5M = loadAvg.Load5
		metrics.LoadAverage_15M = loadAvg.Load15

		c.logger.Debug("macOS load average collected",
			zap.Float64("load1", loadAvg.Load1),
			zap.Float64("load5", loadAvg.Load5),
			zap.Float64("load15", loadAvg.Load15))
	} else {
		c.logger.Warn("Failed to get load average", zap.Error(err))
	}

	// Filter disk partitions - macOS style
	if partitions, err := disk.Partitions(false); err == nil {
		for _, partition := range partitions {
			// Skip macOS-specific virtual filesystems
			if isVirtualFilesystemMacOS(partition.Fstype) {
				continue
			}

			// Skip /dev and other system paths
			if strings.HasPrefix(partition.Mountpoint, "/dev") ||
				strings.HasPrefix(partition.Mountpoint, "/System/Volumes") {
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

	c.logger.Debug("macOS metrics collected",
		zap.Int("disk_count", len(metrics.DiskUsage)))
}

// isVirtualFilesystemMacOS checks if a filesystem type is virtual on macOS
func isVirtualFilesystemMacOS(fstype string) bool {
	virtualFS := []string{
		"devfs", "autofs", "mtmfs", "nullfs",
	}

	for _, vfs := range virtualFS {
		if fstype == vfs {
			return true
		}
	}
	return false
}

// getMacOSVersion gets macOS version using sw_vers command
func getMacOSVersion() (string, error) {
	cmd := exec.Command("/usr/bin/sw_vers", "-productVersion")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	version := strings.TrimSpace(string(output))

	// Get product name
	cmd = exec.Command("/usr/bin/sw_vers", "-productName")
	if nameOutput, err := cmd.Output(); err == nil {
		name := strings.TrimSpace(string(nameOutput))
		return name + " " + version, nil
	}

	return "macOS " + version, nil
}

// getMacOSCPUModel gets CPU model using sysctl command
func getMacOSCPUModel() (string, error) {
	cmd := exec.Command("/usr/sbin/sysctl", "-n", "machdep.cpu.brand_string")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}
