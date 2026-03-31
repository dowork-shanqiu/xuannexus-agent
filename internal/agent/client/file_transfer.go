package client

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"

	pb "github.com/dowork-shanqiu/xuannexus-agent/api/proto/agent"
)

var (
	// retryableErrorPatterns defines network errors that should trigger a retry
	retryableErrorPatterns = []string{
		"connection refused",
		"connection reset",
		"broken pipe",
		"timeout",
		"deadline exceeded",
		"temporarily unavailable",
		"transport closing",
		"transport is closing",
		"eof",
	}
)

const (
	maxChunkSize      = 2 * 1024 * 1024 // 2MB chunks (increased from 1MB for better throughput)
	maxRetries        = 5               // Maximum number of retry attempts (increased from 3)
	baseRetryDelay    = 1 * time.Second // Base delay for exponential backoff
	maxRetryDelay     = 30 * time.Second // Maximum retry delay
)

// calculateRetryDelay implements exponential backoff with jitter
func calculateRetryDelay(attempt int) time.Duration {
	// Exponential backoff: delay = baseDelay * 2^(attempt-1)
	delay := baseRetryDelay * time.Duration(1<<uint(attempt-1))
	
	// Cap at maximum delay
	if delay > maxRetryDelay {
		delay = maxRetryDelay
	}
	
	return delay
}

// UploadFile uploads a file to the server with retry logic
func (c *GRPCClient) UploadFile(ctx context.Context, filePath string) (*pb.FileTransferResponse, error) {
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			// Use exponential backoff for retries
			retryDelay := calculateRetryDelay(attempt - 1)
			
			c.logger.Warn("retrying file upload",
				zap.String("file_path", filePath),
				zap.Int("attempt", attempt),
				zap.Int("max_retries", maxRetries),
				zap.Duration("retry_delay", retryDelay))

			// Wait before retry with exponential backoff
			select {
			case <-time.After(retryDelay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		resp, err := c.uploadFileAttempt(ctx, filePath)
		if err == nil {
			if attempt > 1 {
				c.logger.Info("file upload succeeded after retry",
					zap.String("file_path", filePath),
					zap.Int("attempt", attempt))
			}
			return resp, nil
		}

		lastErr = err
		c.logger.Warn("file upload attempt failed",
			zap.String("file_path", filePath),
			zap.Int("attempt", attempt),
			zap.Error(err))

		// Check if error is retryable
		if !isRetryableError(err) {
			c.logger.Error("non-retryable error, giving up",
				zap.String("file_path", filePath),
				zap.Error(err))
			return nil, err
		}
	}

	return nil, fmt.Errorf("file upload failed after %d attempts: %w", maxRetries, lastErr)
}

// uploadFileAttempt performs a single upload attempt
func (c *GRPCClient) uploadFileAttempt(ctx context.Context, filePath string) (*pb.FileTransferResponse, error) {
	// Open file
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Get file info
	fileInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}
	totalSize := fileInfo.Size()

	// Calculate MD5 checksum
	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return nil, fmt.Errorf("failed to calculate checksum: %w", err)
	}
	checksum := hex.EncodeToString(hash.Sum(nil))

	// Reset file pointer
	if _, err := file.Seek(0, 0); err != nil {
		return nil, fmt.Errorf("failed to reset file pointer: %w", err)
	}

	c.logger.Info("starting file upload",
		zap.String("file_path", filePath),
		zap.Int64("total_size", totalSize),
		zap.String("checksum", checksum))

	// Create upload stream
	stream, err := c.client.UploadFile(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create upload stream: %w", err)
	}

	// Stream file chunks
	buffer := make([]byte, maxChunkSize)
	var offset int64

	for {
		n, err := file.Read(buffer)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read file: %w", err)
		}

		isLast := offset+int64(n) >= totalSize

		chunk := &pb.FileChunk{
			AgentId:     c.cfg.Registration.AgentID,
			Token:       c.cfg.Registration.Token,
			FilePath:    filePath,
			TotalSize:   totalSize,
			Offset:      offset,
			Data:        buffer[:n],
			IsLastChunk: isLast,
		}

		if isLast {
			chunk.Checksum = checksum
		}

		if err := stream.Send(chunk); err != nil {
			return nil, fmt.Errorf("failed to send chunk: %w", err)
		}

		offset += int64(n)

		// Log progress every 10MB or on last chunk
		if offset%(10*1024*1024) == 0 || isLast {
			progress := float64(offset) / float64(totalSize) * 100
			c.logger.Info("upload progress",
				zap.String("file_path", filePath),
				zap.Int64("bytes_sent", offset),
				zap.Float64("progress_percent", progress))
		}
	}

	// Close stream and receive response
	resp, err := stream.CloseAndRecv()
	if err != nil {
		return nil, fmt.Errorf("failed to close stream: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("upload failed: %s", resp.Message)
	}

	c.logger.Info("file upload completed",
		zap.String("file_path", filePath),
		zap.String("file_id", resp.FileId),
		zap.Int64("bytes_received", resp.BytesReceived))

	return resp, nil
}

// DownloadFile downloads a file from the server with retry logic
func (c *GRPCClient) DownloadFile(ctx context.Context, fileID string, destPath string) error {
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			// Use exponential backoff for retries
			retryDelay := calculateRetryDelay(attempt - 1)
			
			c.logger.Warn("retrying file download",
				zap.String("file_id", fileID),
				zap.String("dest_path", destPath),
				zap.Int("attempt", attempt),
				zap.Int("max_retries", maxRetries),
				zap.Duration("retry_delay", retryDelay))

			// Wait before retry with exponential backoff
			select {
			case <-time.After(retryDelay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		err := c.downloadFileAttempt(ctx, fileID, destPath)
		if err == nil {
			if attempt > 1 {
				c.logger.Info("file download succeeded after retry",
					zap.String("file_id", fileID),
					zap.Int("attempt", attempt))
			}
			return nil
		}

		lastErr = err
		c.logger.Warn("file download attempt failed",
			zap.String("file_id", fileID),
			zap.Int("attempt", attempt),
			zap.Error(err))

		// Check if error is retryable
		if !isRetryableError(err) {
			c.logger.Error("non-retryable error, giving up",
				zap.String("file_id", fileID),
				zap.Error(err))
			return err
		}
	}

	return fmt.Errorf("file download failed after %d attempts: %w", maxRetries, lastErr)
}

// downloadFileAttempt performs a single download attempt
func (c *GRPCClient) downloadFileAttempt(ctx context.Context, fileID string, destPath string) error {
	c.logger.Info("starting file download",
		zap.String("file_id", fileID),
		zap.String("dest_path", destPath))

	// Create download stream
	req := &pb.FileDownloadRequest{
		AgentId: c.cfg.Registration.AgentID,
		Token:   c.cfg.Registration.Token,
		FileId:  fileID,
	}

	stream, err := c.client.DownloadFile(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to create download stream: %w", err)
	}

	// Ensure destination directory exists
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Create temporary file
	tempFile := destPath + ".tmp"
	file, err := os.Create(tempFile)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer func() {
		file.Close()
		// Clean up temp file if download fails
		if err != nil {
			os.Remove(tempFile)
		}
	}()

	var (
		totalSize        int64
		bytesReceived    int64
		hash             = md5.New()
		expectedChecksum string
	)

	// Receive file chunks
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to receive chunk: %w", err)
		}

		// First chunk: get total size
		if totalSize == 0 {
			totalSize = chunk.TotalSize
			c.logger.Info("download started",
				zap.String("file_id", fileID),
				zap.Int64("total_size", totalSize))
		}

		// Write chunk to file
		n, err := file.Write(chunk.Data)
		if err != nil {
			return fmt.Errorf("failed to write chunk: %w", err)
		}

		bytesReceived += int64(n)
		hash.Write(chunk.Data)

		// Log progress every 10MB or on last chunk
		if bytesReceived%(10*1024*1024) == 0 || chunk.IsLastChunk {
			progress := float64(bytesReceived) / float64(totalSize) * 100
			c.logger.Info("download progress",
				zap.String("file_id", fileID),
				zap.Int64("bytes_received", bytesReceived),
				zap.Float64("progress_percent", progress))
		}

		// Last chunk: get checksum
		if chunk.IsLastChunk {
			expectedChecksum = chunk.Checksum
			break
		}
	}

	file.Close()

	// Verify checksum if provided
	if expectedChecksum != "" {
		actualChecksum := hex.EncodeToString(hash.Sum(nil))
		if actualChecksum != expectedChecksum {
			os.Remove(tempFile)
			return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, actualChecksum)
		}
		c.logger.Info("checksum verified", zap.String("checksum", actualChecksum))
	}

	// Rename temp file to final destination
	if err := os.Rename(tempFile, destPath); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	c.logger.Info("file download completed",
		zap.String("file_id", fileID),
		zap.String("dest_path", destPath),
		zap.Int64("bytes_received", bytesReceived))

	return nil
}

// isRetryableError determines if an error is retryable
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Network errors, timeouts, and temporary failures are retryable
	errStr := strings.ToLower(err.Error())

	for _, pattern := range retryableErrorPatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	return false
}
