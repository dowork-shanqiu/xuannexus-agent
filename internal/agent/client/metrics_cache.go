package client

import (
	"sync"
	"time"

	"go.uber.org/zap"
	pb "github.com/dowork-shanqiu/xuannexus-agent/api/proto/agent"
)

// MetricsCache caches metrics when server is unreachable
type MetricsCache struct {
	metrics []cachedMetric
	maxSize int
	mu      sync.RWMutex
	logger  *zap.Logger
}

type cachedMetric struct {
	metrics   *pb.DynamicMetrics
	timestamp time.Time
}

// NewMetricsCache creates a new metrics cache
func NewMetricsCache(maxSize int, logger *zap.Logger) *MetricsCache {
	return &MetricsCache{
		metrics: make([]cachedMetric, 0, maxSize),
		maxSize: maxSize,
		logger:  logger,
	}
}

// Add adds metrics to the cache
func (mc *MetricsCache) Add(metrics *pb.DynamicMetrics) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	// Add new metric
	mc.metrics = append(mc.metrics, cachedMetric{
		metrics:   metrics,
		timestamp: time.Now(),
	})

	// Trim if exceeds max size (keep most recent)
	if len(mc.metrics) > mc.maxSize {
		mc.metrics = mc.metrics[len(mc.metrics)-mc.maxSize:]
		mc.logger.Warn("metrics cache full, dropping oldest entries",
			zap.Int("max_size", mc.maxSize))
	}

	mc.logger.Debug("metrics cached",
		zap.Int("cache_size", len(mc.metrics)))
}

// GetAll returns all cached metrics and clears the cache
func (mc *MetricsCache) GetAll() []*pb.DynamicMetrics {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if len(mc.metrics) == 0 {
		return nil
	}

	result := make([]*pb.DynamicMetrics, len(mc.metrics))
	for i, cm := range mc.metrics {
		result[i] = cm.metrics
	}

	// Clear cache after retrieving
	mc.metrics = mc.metrics[:0]

	mc.logger.Info("retrieved cached metrics",
		zap.Int("count", len(result)))

	return result
}

// Size returns the current cache size
func (mc *MetricsCache) Size() int {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return len(mc.metrics)
}

// Clear clears all cached metrics
func (mc *MetricsCache) Clear() {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.metrics = mc.metrics[:0]
	mc.logger.Debug("metrics cache cleared")
}

// GetOldest returns the age of the oldest cached metric
func (mc *MetricsCache) GetOldest() time.Duration {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	if len(mc.metrics) == 0 {
		return 0
	}

	return time.Since(mc.metrics[0].timestamp)
}
