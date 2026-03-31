//go:build !linux && !windows && !darwin

package collector

import (
	pb "github.com/dowork-shanqiu/xuannexus-agent/api/proto/agent"
)

// CollectPlatformSpecificStaticInfo is a no-op for unsupported platforms
func (c *SystemCollector) CollectPlatformSpecificStaticInfo(info *pb.StaticInfo) {
	// No platform-specific collection for this OS
	c.logger.Debug("No platform-specific static info collector for this OS")
}

// CollectPlatformSpecificDynamicMetrics is a no-op for unsupported platforms
func (c *SystemCollector) CollectPlatformSpecificDynamicMetrics(metrics *pb.DynamicMetrics) {
	// No platform-specific collection for this OS
	c.logger.Debug("No platform-specific dynamic metrics collector for this OS")
}
