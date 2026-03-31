package client

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	pb "github.com/dowork-shanqiu/xuannexus-agent/api/proto/agent"
)

// CommandListener listens for commands from server via gRPC streaming
type CommandListener struct {
	client          *GRPCClient
	logger          *zap.Logger
	mu              sync.RWMutex
	running         map[string]*exec.Cmd // executionID -> command
	stopChan        chan struct{}
	agentID         string
	token           string
	sandboxExecutor *SandboxExecutor // Sandbox executor for secure command execution
	collector       MetricsCollector // System metrics collector
}

// MetricsCollector interface for collecting system metrics
type MetricsCollector interface {
	CollectDynamicMetrics() *pb.DynamicMetrics
}

// NewCommandListener creates a new command listener
func NewCommandListener(client *GRPCClient, agentID, token string, logger *zap.Logger, collector MetricsCollector) *CommandListener {
	// Initialize with default sandbox configuration
	sandboxConfig := DefaultSandboxConfig()
	sandboxExecutor, err := NewSandboxExecutor(sandboxConfig)
	if err != nil {
		logger.Warn("Failed to initialize sandbox executor, running without sandbox",
			zap.Error(err))
		// Create a disabled sandbox as fallback
		sandboxConfig.Enabled = false
		sandboxExecutor, _ = NewSandboxExecutor(sandboxConfig)
	}

	return &CommandListener{
		client:          client,
		logger:          logger,
		running:         make(map[string]*exec.Cmd),
		stopChan:        make(chan struct{}),
		agentID:         agentID,
		token:           token,
		sandboxExecutor: sandboxExecutor,
		collector:       collector,
	}
}

// Start starts listening for commands from server
func (cl *CommandListener) Start(ctx context.Context) {
	cl.logger.Info("Command listener started")

	for {
		select {
		case <-ctx.Done():
			cl.logger.Info("Command listener stopping (context done)")
			return
		case <-cl.stopChan:
			cl.logger.Info("Command listener stopping")
			return
		default:
			if err := cl.listenForCommands(ctx); err != nil {
				cl.logger.Error("Command listener error", zap.Error(err))
				// Wait before reconnecting
				select {
				case <-time.After(5 * time.Second):
				case <-ctx.Done():
					return
				case <-cl.stopChan:
					return
				}
			}
		}
	}
}

// listenForCommands establishes a bidirectional stream and listens for incoming commands
func (cl *CommandListener) listenForCommands(ctx context.Context) error {
	cl.logger.Info("Establishing bidirectional command stream with server")

	// Create bidirectional streaming client
	stream, err := cl.client.client.ExecuteCommand(ctx)
	if err != nil {
		return fmt.Errorf("failed to establish command stream: %w", err)
	}

	// Send initial auth message
	authMsg := &pb.CommandMessage{
		Type: "auth",
		CommandRequest: &pb.CommandRequest{
			AgentId: cl.agentID,
			Token:   cl.token,
		},
	}

	if err := stream.Send(authMsg); err != nil {
		return fmt.Errorf("failed to send auth message: %w", err)
	}

	cl.logger.Info("Bidirectional command stream established")

	// Immediately report dynamic metrics after connection
	if cl.collector != nil {
		go func() {
			cl.logger.Info("Immediately uploading dynamic metrics after connection")
			metrics := cl.collector.CollectDynamicMetrics()
			if err := cl.client.ReportDynamicMetrics(metrics); err != nil {
				cl.logger.Error("Failed to upload initial dynamic metrics", zap.Error(err))
			} else {
				cl.logger.Info("Initial dynamic metrics uploaded successfully")
			}
		}()
	}

	// Listen for commands from server
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			cl.logger.Info("Command stream closed by server")
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to receive command message: %w", err)
		}

		// Handle received message
		if msg.Type == "command" && msg.CommandRequest != nil {
			cl.logger.Info("Received command from server",
				zap.String("execution_id", msg.CommandRequest.ExecutionId),
				zap.String("command", msg.CommandRequest.Command))

			// Execute command asynchronously and report via stream
			go cl.executeCommand(ctx, msg.CommandRequest, stream)
		}
	}
}

// executeCommand executes a single command and reports results via stream
func (cl *CommandListener) executeCommand(ctx context.Context, cmdReq *pb.CommandRequest, stream pb.AgentService_ExecuteCommandClient) {
	executionID := cmdReq.ExecutionId
	cl.logger.Info("Executing command",
		zap.String("execution_id", executionID),
		zap.String("command", cmdReq.Command))

	startTime := time.Now()

	// Validate command before execution
	validator := NewCommandValidator()
	if err := validator.ValidateCommand(cmdReq.Command); err != nil {
		cl.logger.Error("Command validation failed",
			zap.String("execution_id", executionID),
			zap.String("command", cmdReq.Command),
			zap.Error(err))

		// Report failure
		cl.reportCompletion(stream, executionID, "failed", 1, "", "", err.Error(), time.Since(startTime))
		return
	}

	// Log warning for high-risk commands
	if validator.ShouldWarn(cmdReq.Command) {
		riskLevel := validator.GetRiskLevel(cmdReq.Command)
		cl.logger.Warn("Executing high-risk command",
			zap.String("execution_id", executionID),
			zap.String("command", cmdReq.Command),
			zap.String("risk_level", riskLevel))
	}

	// Check for special file download command
	if cmdReq.Command == "__download_file__" {
		cl.handleFileDownload(ctx, cmdReq, stream, executionID, startTime)
		return
	}

	// Validate command against sandbox rules
	if err := cl.sandboxExecutor.ValidateCommand(cmdReq.Command, cmdReq.WorkingDir); err != nil {
		cl.logger.Error("Sandbox validation failed",
			zap.String("execution_id", executionID),
			zap.String("command", cmdReq.Command),
			zap.Error(err))

		// Report failure
		cl.reportCompletion(stream, executionID, "failed", 1, "", "", err.Error(), time.Since(startTime))
		return
	}

	// Use sandbox executor to prepare the command
	// Note: We use context.Background() instead of the parent context to avoid "context canceled" 
	// errors during cmd.Start(). The command needs to start successfully even if there's a timeout,
	// and we enforce the timeout/cancellation during cmd.Wait() instead. The parent context 
	// cancellation is still handled in the select statement below.
	cmd, err := cl.sandboxExecutor.PrepareCommand(context.Background(), cmdReq.Command, cmdReq.Args, cmdReq.WorkingDir, cmdReq.Env)
	if err != nil {
		cl.logger.Error("Failed to prepare sandboxed command",
			zap.String("execution_id", executionID),
			zap.String("command", cmdReq.Command),
			zap.Error(err))

		// Report failure
		cl.reportCompletion(stream, executionID, "failed", 1, "", "", err.Error(), time.Since(startTime))
		return
	}

	// Track running command
	cl.mu.Lock()
	cl.running[executionID] = cmd
	cl.mu.Unlock()

	defer func() {
		cl.mu.Lock()
		delete(cl.running, executionID)
		cl.mu.Unlock()
	}()

	// Capture stdout and stderr
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cl.logger.Error("Failed to create stdout pipe", zap.Error(err))
		cl.reportCompletion(stream, executionID, "failed", -1, "", "", err.Error(), time.Since(startTime))
		return
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		cl.logger.Error("Failed to create stderr pipe", zap.Error(err))
		cl.reportCompletion(stream, executionID, "failed", -1, "", "", err.Error(), time.Since(startTime))
		return
	}

	// Start command
	if err := cmd.Start(); err != nil {
		cl.logger.Error("Failed to start command", zap.Error(err))
		cl.reportCompletion(stream, executionID, "failed", -1, "", "", err.Error(), time.Since(startTime))
		return
	}

	// Read output
	var stdout, stderr strings.Builder
	var wg sync.WaitGroup

	wg.Add(2)

	// Read stdout
	go func() {
		defer wg.Done()
		// Use encoding scanner to handle system encoding (Windows code pages, etc.)
		scanner := NewEncodingScanner(stdoutPipe)
		for scanner.Scan() {
			line := scanner.Text()
			stdout.WriteString(line)
			stdout.WriteString("\n")
			// Limit to 10KB
			if stdout.Len() > 10*1024 {
				break
			}
		}
	}()

	// Read stderr
	go func() {
		defer wg.Done()
		// Use encoding scanner to handle system encoding (Windows code pages, etc.)
		scanner := NewEncodingScanner(stderrPipe)
		for scanner.Scan() {
			line := scanner.Text()
			stderr.WriteString(line)
			stderr.WriteString("\n")
			// Limit to 10KB
			if stderr.Len() > 10*1024 {
				break
			}
		}
	}()

	// Wait for command to complete with timeout
	wg.Wait()
	
	// Create timeout channel if timeout is specified
	// Note: If cmdReq.TimeoutSeconds is 0 or negative, timeoutChan will be nil,
	// which causes the select statement to skip the timeout case (nil channels block forever).
	// This is the intended behavior - no timeout enforcement when timeout is not set.
	var timeoutChan <-chan time.Time
	if cmdReq.TimeoutSeconds > 0 {
		timeoutChan = time.After(time.Duration(cmdReq.TimeoutSeconds) * time.Second)
	}
	
	// Wait for command completion or timeout
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	
	// Reuse err variable from earlier declaration
	select {
	case err = <-done:
		// Command completed normally
	case <-timeoutChan:
		// Timeout occurred - kill the process
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		<-done // Wait for Wait() to return
		err = fmt.Errorf("command timeout after %d seconds", cmdReq.TimeoutSeconds)
	case <-ctx.Done():
		// Parent context canceled - kill the process and wrap error with context
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		<-done // Wait for Wait() to return
		err = fmt.Errorf("command canceled due to context cancellation: %w", ctx.Err())
	}

	duration := time.Since(startTime)
	exitCode := 0
	status := "completed"
	errorMsg := ""

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			if exitCode != 0 {
				status = "failed"
				errorMsg = fmt.Sprintf("Command exited with code %d", exitCode)
			}
		} else {
			status = "failed"
			exitCode = -1
			errorMsg = err.Error()
		}
	}

	cl.logger.Info("Command completed",
		zap.String("execution_id", executionID),
		zap.String("status", status),
		zap.Int("exit_code", exitCode),
		zap.Duration("duration", duration))

	// Report completion via stream
	cl.reportCompletion(stream, executionID, status, exitCode, stdout.String(), stderr.String(), errorMsg, duration)
}


// handleFileDownload handles the special __download_file__ command
func (cl *CommandListener) handleFileDownload(ctx context.Context, cmdReq *pb.CommandRequest, stream pb.AgentService_ExecuteCommandClient, executionID string, startTime time.Time) {
	// Parse args: [fileID, destPath]
	if len(cmdReq.Args) < 2 {
		cl.logger.Error("Invalid file download command - missing args",
			zap.String("execution_id", executionID),
			zap.Strings("args", cmdReq.Args))
		cl.reportCompletion(stream, executionID, "failed", -1, "", "", "invalid args: expected [fileID, destPath]", time.Since(startTime))
		return
	}

	fileID := cmdReq.Args[0]
	destPath := cmdReq.Args[1]

	cl.logger.Info("Downloading file",
		zap.String("execution_id", executionID),
		zap.String("file_id", fileID),
		zap.String("dest_path", destPath))

	// Use the DownloadFile method from the gRPC client
	err := cl.client.DownloadFile(ctx, fileID, destPath)
	
	duration := time.Since(startTime)
	
	if err != nil {
		cl.logger.Error("File download failed",
			zap.String("execution_id", executionID),
			zap.String("file_id", fileID),
			zap.Error(err))
		cl.reportCompletion(stream, executionID, "failed", -1, "", "", fmt.Sprintf("download failed: %v", err), duration)
		return
	}

	cl.logger.Info("File download completed",
		zap.String("execution_id", executionID),
		zap.String("file_id", fileID),
		zap.String("dest_path", destPath))

	stdout := fmt.Sprintf("Downloaded file %s to %s", fileID, destPath)
	cl.reportCompletion(stream, executionID, "completed", 0, stdout, "", "", duration)
}

// reportCompletion reports command completion to server via gRPC stream
func (cl *CommandListener) reportCompletion(stream pb.AgentService_ExecuteCommandClient, executionID, status string, exitCode int,
	stdout, stderr, errorMsg string, duration time.Duration) {

	// Sanitize all string fields to ensure valid UTF-8 (final safety check)
	stdout = sanitizeUTF8String(stdout)
	stderr = sanitizeUTF8String(stderr)
	errorMsg = sanitizeUTF8String(errorMsg)

	completion := &pb.CommandCompletion{
		ExecutionId:   executionID,
		Status:        status,
		ExitCode:      int32(exitCode),
		StdoutPreview: stdout,
		StderrPreview: stderr,
		ErrorMessage:  errorMsg,
		DurationMs:    duration.Milliseconds(),
		Timestamp:     time.Now().UnixMilli(),
	}

	msg := &pb.CommandMessage{
		Type:              "completion",
		CommandCompletion: completion,
	}

	if err := stream.Send(msg); err != nil {
		cl.logger.Error("Failed to send completion message",
			zap.String("execution_id", executionID),
			zap.Error(err))
		return
	}

	cl.logger.Info("Command completion reported via stream",
		zap.String("execution_id", executionID),
		zap.String("status", status))
}

// Stop stops the command listener and kills all running commands
func (cl *CommandListener) Stop() {
	cl.logger.Info("Stopping command listener and killing running commands")

	close(cl.stopChan)

	cl.mu.Lock()
	defer cl.mu.Unlock()

	for executionID, cmd := range cl.running {
		if cmd.Process != nil {
			cl.logger.Info("Killing running command", zap.String("execution_id", executionID))
			cmd.Process.Kill()
		}
	}

	cl.running = make(map[string]*exec.Cmd)
}
