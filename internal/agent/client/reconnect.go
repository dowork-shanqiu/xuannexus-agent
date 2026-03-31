package client

import (
	"context"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"github.com/dowork-shanqiu/xuannexus-agent/internal/agent/config"
)

// ReconnectManager manages automatic reconnection for the gRPC client
type ReconnectManager struct {
	client       *GRPCClient
	config       *config.Config
	logger       *zap.Logger
	maxRetries   int           // Maximum number of retries (0 = unlimited)
	baseDelay    time.Duration // Base delay between retries (1s)
	maxDelay     time.Duration // Maximum delay between retries (30s)
	connected    atomic.Bool   // Connection status
	reconnecting atomic.Bool   // Reconnecting flag
	stopChan     chan struct{}
	// onConnected is called after each successful reconnection (not on initial startup)
	// to allow the caller to re-send static info, dynamic metrics, etc.
	onConnected func()
}

// NewReconnectManager creates a new reconnect manager.
// onConnected (optional) is invoked in a new goroutine after every successful
// reconnection so the caller can re-send static info and initial metrics.
func NewReconnectManager(client *GRPCClient, cfg *config.Config, logger *zap.Logger, maxRetries int, onConnected func()) *ReconnectManager {
	rm := &ReconnectManager{
		client:      client,
		config:      cfg,
		logger:      logger,
		maxRetries:  maxRetries,
		baseDelay:   1 * time.Second,
		maxDelay:    30 * time.Second,
		stopChan:    make(chan struct{}),
		onConnected: onConnected,
	}
	rm.connected.Store(true)
	return rm
}

// Start starts the reconnect manager monitoring
func (rm *ReconnectManager) Start(ctx context.Context) {
	rm.logger.Info("reconnect manager started")

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			rm.logger.Info("reconnect manager stopped")
			return
		case <-rm.stopChan:
			rm.logger.Info("reconnect manager stopped")
			return
		case <-ticker.C:
			// Check connection health by attempting a heartbeat
			if rm.connected.Load() {
				err := rm.client.SendHeartbeat()
				if err != nil {
					rm.logger.Warn("heartbeat failed, triggering reconnect", zap.Error(err))
					rm.connected.Store(false)
					go rm.Reconnect(ctx)
				}
			}
		}
	}
}

// Reconnect attempts to reconnect to the server with exponential backoff
func (rm *ReconnectManager) Reconnect(ctx context.Context) {
	if rm.reconnecting.Swap(true) {
		// Already reconnecting
		return
	}
	defer rm.reconnecting.Store(false)

	rm.logger.Info("starting reconnection process")

	attempt := 0
	for {
		attempt++

		// Check if max retries reached
		if rm.maxRetries > 0 && attempt > rm.maxRetries {
			rm.logger.Error("max reconnection attempts reached", zap.Int("attempts", attempt))
			return
		}

		// Calculate delay with exponential backoff
		delay := rm.calculateDelay(attempt)
		rm.logger.Info("attempting reconnection",
			zap.Int("attempt", attempt),
			zap.Duration("delay", delay))

		// Wait before retry
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			rm.logger.Info("reconnection cancelled")
			return
		case <-rm.stopChan:
			rm.logger.Info("reconnection stopped")
			return
		}

		// Close old connection
		rm.client.Close()

		// Create new connection
		err := rm.client.Connect()
		if err != nil {
			rm.logger.Warn("reconnection failed", zap.Error(err), zap.Int("attempt", attempt))
			continue
		}

		// Re-authenticate if we have credentials
		if rm.config.Registration.AgentID != "" && rm.config.Registration.Token != "" {
			rm.logger.Info("re-authenticating with existing credentials")
			// Test authentication with a heartbeat
			err = rm.client.SendHeartbeat()
			if err != nil {
				rm.logger.Warn("re-authentication failed", zap.Error(err))
				continue
			}
		} else {
			rm.logger.Warn("no credentials available, agent needs to register again")
			// In production, you might want to trigger re-registration here
			// For now, we'll just mark as connected and let the main loop handle it
		}

		// Reconnection successful
		rm.connected.Store(true)
		rm.logger.Info("reconnection successful", zap.Int("attempt", attempt))

		// Invoke the onConnected callback so the caller can re-send static
		// info, initial dynamic metrics, etc.
		if rm.onConnected != nil {
			go rm.onConnected()
		}

		return
	}
}

// calculateDelay calculates the delay for the next retry using exponential backoff
func (rm *ReconnectManager) calculateDelay(attempt int) time.Duration {
	// Exponential backoff: 1s, 2s, 4s, 8s, 16s, 30s (max)
	delay := rm.baseDelay
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay > rm.maxDelay {
			delay = rm.maxDelay
			break
		}
	}
	return delay
}

// IsConnected returns the current connection status
func (rm *ReconnectManager) IsConnected() bool {
	return rm.connected.Load()
}

// Stop stops the reconnect manager
func (rm *ReconnectManager) Stop() {
	close(rm.stopChan)
}
