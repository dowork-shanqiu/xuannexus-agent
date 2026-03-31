package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"runtime"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	pb "github.com/dowork-shanqiu/xuannexus-agent/api/proto/agent"
	"github.com/dowork-shanqiu/xuannexus-agent/internal/agent/config"
)

// GRPCClient is the gRPC client for agent
type GRPCClient struct {
	cfg          *config.Config
	logger       *zap.Logger
	conn         *grpc.ClientConn
	client       pb.AgentServiceClient
	metricsCache *MetricsCache
}

// NewGRPCClient creates a new gRPC client
func NewGRPCClient(cfg *config.Config, logger *zap.Logger) (*GRPCClient, error) {
	// Create HTTP client (will be initialized after registration)
	return &GRPCClient{
		cfg:          cfg,
		logger:       logger,
		metricsCache: NewMetricsCache(100, logger), // Cache up to 100 metrics
	}, nil
}

// Connect establishes a connection to the server
func (c *GRPCClient) Connect() error {
	var opts []grpc.DialOption

	// Setup TLS if enabled
	if c.cfg.Server.TLS.Enable {
		tlsConfig, err := c.loadTLSConfig()
		if err != nil {
			return fmt.Errorf("failed to load TLS config: %w", err)
		}
		creds := credentials.NewTLS(tlsConfig)
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	// Set max message size
	opts = append(opts,
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(10*1024*1024), // 10MB
			grpc.MaxCallSendMsgSize(10*1024*1024), // 10MB
		),
	)

	// Create connection
	ctx, cancel := context.WithTimeout(context.Background(), c.cfg.Server.Timeout)
	defer cancel()

	conn, err := grpc.DialContext(ctx, c.cfg.Server.Address, opts...)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	c.conn = conn
	c.client = pb.NewAgentServiceClient(conn)

	return nil
}

// Close closes the connection
func (c *GRPCClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// Register registers the agent with the server
// Returns: agent_id, token, error
func (c *GRPCClient) Register(version string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.cfg.Server.Timeout)
	defer cancel()

	// Auto-detect OS type
	osType := runtime.GOOS
	if osType == "darwin" {
		osType = "macos"
	}

	// Send registration key, OS type, and version
	req := &pb.RegisterRequest{
		RegistrationKey: c.cfg.Registration.Key,
		OsType:          osType,
		AgentVersion:    version,
	}

	resp, err := c.client.Register(ctx, req)
	if err != nil {
		return "", "", fmt.Errorf("failed to register: %w", err)
	}

	if !resp.Success {
		return "", "", fmt.Errorf("registration failed: %s", resp.Message)
	}

	// Log received metadata from pre-created agent
	c.logger.Info("Received agent metadata from server",
		zap.String("hostname", resp.Hostname),
		zap.String("cloud_provider", resp.CloudProvider),
		zap.String("region", resp.Region))

	return resp.AgentId, resp.Token, nil
}

// SendHeartbeat sends a heartbeat to the server
func (c *GRPCClient) SendHeartbeat() error {
	ctx, cancel := context.WithTimeout(context.Background(), c.cfg.Server.Timeout)
	defer cancel()

	// Add authentication metadata
	ctx = c.withAuth(ctx)

	req := &pb.HeartbeatRequest{
		AgentId:   c.cfg.Registration.AgentID,
		Token:     c.cfg.Registration.Token,
		Timestamp: time.Now().Unix(),
		Status:    "running",
	}

	_, err := c.client.Heartbeat(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to send heartbeat: %w", err)
	}

	return nil
}

// ReportStaticInfo reports static system information to the server
func (c *GRPCClient) ReportStaticInfo(info *pb.StaticInfo) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.cfg.Server.Timeout)
	defer cancel()

	// Wrap in request message with auth
	req := &pb.StaticInfoRequest{
		AgentId: c.cfg.Registration.AgentID,
		Token:   c.cfg.Registration.Token,
		Info:    info,
	}

	_, err := c.client.ReportStaticInfo(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to report static info: %w", err)
	}

	return nil
}

// ReportDynamicMetrics reports dynamic metrics to the server with caching on failure
func (c *GRPCClient) ReportDynamicMetrics(metrics *pb.DynamicMetrics) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.cfg.Server.Timeout)
	defer cancel()

	// Wrap in request message with auth
	req := &pb.DynamicMetricsRequest{
		AgentId: c.cfg.Registration.AgentID,
		Token:   c.cfg.Registration.Token,
		Metrics: metrics,
	}

	_, err := c.client.ReportDynamicMetrics(ctx, req)
	if err != nil {
		// Cache metrics if server is unreachable
		c.logger.Warn("failed to report metrics, caching for later",
			zap.Error(err),
			zap.Int("cache_size", c.metricsCache.Size()))
		c.metricsCache.Add(metrics)
		return fmt.Errorf("failed to report metrics (cached): %w", err)
	}

	// If successful and we have cached metrics, try to send them
	if c.metricsCache.Size() > 0 {
		c.logger.Info("server reconnected, sending cached metrics",
			zap.Int("cached_count", c.metricsCache.Size()))
		go c.flushCachedMetrics()
	}

	return nil
}

// flushCachedMetrics sends all cached metrics to the server
func (c *GRPCClient) flushCachedMetrics() {
	cached := c.metricsCache.GetAll()
	if len(cached) == 0 {
		return
	}

	successCount := 0
	failCount := 0
	var failedMetrics []*pb.DynamicMetrics

	for _, metrics := range cached {
		ctx, cancel := context.WithTimeout(context.Background(), c.cfg.Server.Timeout)
		req := &pb.DynamicMetricsRequest{
			AgentId: c.cfg.Registration.AgentID,
			Token:   c.cfg.Registration.Token,
			Metrics: metrics,
		}

		_, err := c.client.ReportDynamicMetrics(ctx, req)
		cancel()

		if err != nil {
			failCount++
			c.logger.Warn("failed to send cached metric",
				zap.Error(err))
			// Collect failed metrics to re-cache at end
			failedMetrics = append(failedMetrics, metrics)
		} else {
			successCount++
		}
	}

	// Re-cache only the metrics that failed to send
	for _, metrics := range failedMetrics {
		c.metricsCache.Add(metrics)
	}

	c.logger.Info("finished flushing cached metrics",
		zap.Int("success", successCount),
		zap.Int("failed", failCount),
		zap.Int("re_cached", len(failedMetrics)))
}

// withAuth adds authentication metadata to context
func (c *GRPCClient) withAuth(ctx context.Context) context.Context {
	md := metadata.New(map[string]string{
		"agent-id": c.cfg.Registration.AgentID,
		"token":    c.cfg.Registration.Token,
	})
	return metadata.NewOutgoingContext(ctx, md)
}

// loadTLSConfig loads TLS configuration
func (c *GRPCClient) loadTLSConfig() (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: c.cfg.Server.TLS.InsecureSkipVerify, //nolint:gosec // 由配置控制，文档说明仅用于测试
	}

	// Load CA certificate if provided（用于验证服务端证书）
	if c.cfg.Server.TLS.CACert != "" {
		caCert, err := os.ReadFile(c.cfg.Server.TLS.CACert)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}

		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to append CA certificate")
		}
		tlsConfig.RootCAs = certPool
	}

	// Load client certificate if provided（mTLS 双向认证）
	if c.cfg.Server.TLS.ClientCert != "" && c.cfg.Server.TLS.ClientKey != "" {
		cert, err := tls.LoadX509KeyPair(c.cfg.Server.TLS.ClientCert, c.cfg.Server.TLS.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}
