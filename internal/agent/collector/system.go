package collector

import (
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"
	"go.uber.org/zap"
	pb "github.com/dowork-shanqiu/xuannexus-agent/api/proto/agent"
)

// SystemCollector collects system information
type SystemCollector struct {
	logger *zap.Logger
}

// NewSystemCollector creates a new system collector
func NewSystemCollector(logger *zap.Logger) *SystemCollector {
	return &SystemCollector{
		logger: logger,
	}
}

// CollectStaticInfo collects static system information
func (c *SystemCollector) CollectStaticInfo() *pb.StaticInfo {
	info := &pb.StaticInfo{
		Architecture: runtime.GOARCH,
		OsType:       runtime.GOOS,
	}

	// Get hostname
	if hostname, err := os.Hostname(); err == nil {
		info.Hostname = hostname
	} else {
		c.logger.Warn("Failed to get hostname", zap.Error(err))
		info.Hostname = "unknown"
	}

	// Get CPU information
	if cpuInfo, err := cpu.Info(); err == nil && len(cpuInfo) > 0 {
		info.CpuModel = cpuInfo[0].ModelName
		info.CpuCores = cpuInfo[0].Cores
	} else {
		c.logger.Warn("Failed to get CPU info", zap.Error(err))
		info.CpuModel = "Unknown"
		info.CpuCores = int32(runtime.NumCPU())
	}

	// Get memory information
	if vmem, err := mem.VirtualMemory(); err == nil {
		info.TotalMemory = int64(vmem.Total) // in bytes
	} else {
		c.logger.Warn("Failed to get memory info", zap.Error(err))
		info.TotalMemory = 0
	}

	// Get disk information (total across all partitions)
	if partitions, err := disk.Partitions(false); err == nil {
		var totalDisk uint64
		for _, partition := range partitions {
			if usage, err := disk.Usage(partition.Mountpoint); err == nil {
				totalDisk += usage.Total
			}
		}
		info.TotalDisk = int64(totalDisk) // in bytes
	} else {
		c.logger.Warn("Failed to get disk info", zap.Error(err))
		info.TotalDisk = 0
	}

	// Get OS information
	if hostInfo, err := host.Info(); err == nil {
		info.OsVersion = hostInfo.PlatformVersion
		info.KernelVersion = hostInfo.KernelVersion
		info.BootTime = int64(hostInfo.BootTime)
	} else {
		c.logger.Warn("Failed to get host info", zap.Error(err))
		info.OsVersion = "unknown"
		info.KernelVersion = "unknown"
		info.BootTime = 0
	}

	// Get network interfaces
	if interfaces, err := net.Interfaces(); err == nil {
		for _, iface := range interfaces {
			// Skip loopback and down interfaces
			if strings.HasPrefix(iface.Name, "lo") || len(iface.Addrs) == 0 {
				continue
			}

			var ipAddresses []string
			for _, addr := range iface.Addrs {
				ipAddresses = append(ipAddresses, addr.Addr)
			}

			info.NetworkInterfaces = append(info.NetworkInterfaces, &pb.NetworkInterface{
				Name:        iface.Name,
				IpAddresses: ipAddresses,
				MacAddress:  iface.HardwareAddr,
				Mtu:         int64(iface.MTU),
			})
		}
	} else {
		c.logger.Warn("Failed to get network interfaces", zap.Error(err))
	}

	// Call platform-specific collector for additional information
	c.CollectPlatformSpecificStaticInfo(info)

	return info
}

// CollectDynamicMetrics collects dynamic system metrics
func (c *SystemCollector) CollectDynamicMetrics() *pb.DynamicMetrics {
	metrics := &pb.DynamicMetrics{
		Timestamp: time.Now().Unix(),
	}

	// Get CPU usage
	if percent, err := cpu.Percent(time.Second, false); err == nil && len(percent) > 0 {
		metrics.CpuUsagePercent = percent[0]
	} else {
		c.logger.Warn("Failed to get CPU usage", zap.Error(err))
		metrics.CpuUsagePercent = 0
	}

	// Get memory usage
	if vmem, err := mem.VirtualMemory(); err == nil {
		metrics.MemoryUsed = int64(vmem.Used)
		metrics.MemoryAvailable = int64(vmem.Available)
		metrics.MemoryUsagePercent = vmem.UsedPercent
	} else {
		c.logger.Warn("Failed to get memory usage", zap.Error(err))
	}

	// Get disk usage for all partitions
	if partitions, err := disk.Partitions(false); err == nil {
		for _, partition := range partitions {
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
	} else {
		c.logger.Warn("Failed to get disk usage", zap.Error(err))
	}

	// Get network statistics
	if ioCounters, err := net.IOCounters(true); err == nil {
		for _, io := range ioCounters {
			// Skip loopback interfaces
			if strings.HasPrefix(io.Name, "lo") {
				continue
			}

			metrics.NetworkStats = append(metrics.NetworkStats, &pb.NetworkStats{
				InterfaceName: io.Name,
				BytesSent:     int64(io.BytesSent),
				BytesRecv:     int64(io.BytesRecv),
				PacketsSent:   int64(io.PacketsSent),
				PacketsRecv:   int64(io.PacketsRecv),
				ErrorsIn:      int64(io.Errin),
				ErrorsOut:     int64(io.Errout),
			})
		}
	} else {
		c.logger.Warn("Failed to get network stats", zap.Error(err))
	}

	// Get process count
	if procs, err := process.Pids(); err == nil {
		metrics.ProcessCount = int32(len(procs))
	} else {
		c.logger.Warn("Failed to get process count", zap.Error(err))
	}

	// Get load average (Unix-like systems only)
	if loadAvg, err := load.Avg(); err == nil {
		metrics.LoadAverage_1M = loadAvg.Load1
		metrics.LoadAverage_5M = loadAvg.Load5
		metrics.LoadAverage_15M = loadAvg.Load15
	} else {
		// On Windows, load average is not available
		if runtime.GOOS != "windows" {
			c.logger.Warn("Failed to get load average", zap.Error(err))
		}
		metrics.LoadAverage_1M = 0
		metrics.LoadAverage_5M = 0
		metrics.LoadAverage_15M = 0
	}

	// Call platform-specific collector for additional metrics and filtering
	c.CollectPlatformSpecificDynamicMetrics(metrics)

	return metrics
}
