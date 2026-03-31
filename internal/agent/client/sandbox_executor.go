package client

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// SandboxExecutor handles sandboxed command execution
type SandboxExecutor struct {
	config *SandboxConfig
}

// NewSandboxExecutor creates a new sandbox executor with the given configuration
func NewSandboxExecutor(config *SandboxConfig) (*SandboxExecutor, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid sandbox config: %w", err)
	}

	return &SandboxExecutor{
		config: config,
	}, nil
}

// PrepareCommand prepares a command with sandbox restrictions
func (se *SandboxExecutor) PrepareCommand(ctx context.Context, command string, args []string, workingDir string, env map[string]string) (*exec.Cmd, error) {
	if !se.config.Enabled {
		// No sandbox, execute normally
		return se.prepareNormalCommand(ctx, command, args, workingDir, env)
	}

	// Apply sandbox restrictions
	return se.prepareSandboxedCommand(ctx, command, args, workingDir, env)
}

// prepareNormalCommand prepares a command without sandbox (legacy behavior)
func (se *SandboxExecutor) prepareNormalCommand(ctx context.Context, command string, args []string, workingDir string, env map[string]string) (*exec.Cmd, error) {
	// Note: We do NOT use exec.CommandContext here because:
	// 1. The timeout is handled by the caller (command_listener.go) during cmd.Wait()
	// 2. Using CommandContext can cause "context canceled" errors during cmd.Start()
	// 3. The ctx parameter is context.Background() from the caller
	var cmd *exec.Cmd

	if runtime.GOOS == "windows" {
		// Windows: use cmd.exe
		// Set UTF-8 code page (65001) for better encoding support.
		// The chcp command is a safe fixed string we control. The & separator
		// ensures chcp runs first, then the user command executes.
		// Command validation happens upstream before reaching this point.
		cmdArgs := []string{"/C", "chcp 65001 >nul 2>&1 & " + command}
		cmdArgs = append(cmdArgs, args...)
		cmd = exec.Command("cmd.exe", cmdArgs...)
	} else {
		// Unix: use sh
		cmdStr := command
		if len(args) > 0 {
			cmdStr += " " + strings.Join(args, " ")
		}
		cmd = exec.Command("sh", "-c", cmdStr)
	}

	// Set working directory
	if workingDir != "" {
		cmd.Dir = workingDir
	}

	// Set environment variables
	if len(env) > 0 {
		envList := make([]string, 0, len(env))
		for k, v := range env {
			envList = append(envList, fmt.Sprintf("%s=%s", k, v))
		}
		cmd.Env = envList
	}

	return cmd, nil
}

// prepareSandboxedCommand prepares a command with sandbox restrictions
func (se *SandboxExecutor) prepareSandboxedCommand(ctx context.Context, command string, args []string, workingDir string, env map[string]string) (*exec.Cmd, error) {
	// Parse the command to extract executable
	cmdParts := strings.Fields(command)
	if len(cmdParts) == 0 {
		return nil, fmt.Errorf("empty command")
	}

	executable := cmdParts[0]

	// Validate executable path
	if err := se.config.ValidateExecutable(executable); err != nil {
		return nil, fmt.Errorf("沙箱限制: %w", err)
	}

	// Validate working directory
	if workingDir != "" {
		if err := se.config.ValidatePath(workingDir); err != nil {
			return nil, fmt.Errorf("沙箱限制: 工作目录 - %w", err)
		}
	}

	// Note: We do NOT use exec.CommandContext here because:
	// 1. The timeout is already handled by the caller (command_listener.go) during cmd.Wait()
	// 2. Using CommandContext with a timeout that gets canceled via defer can cause
	//    "context canceled" errors during cmd.Start() on Windows
	// 3. The ctx parameter is context.Background() from the caller, so we just create
	//    a regular exec.Command without context attachment
	
	// Build command
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		// Windows: use cmd.exe
		// Set UTF-8 code page (65001) for better encoding support.
		// The chcp command is a safe fixed string we control. The & separator
		// ensures chcp runs first, then the user command executes.
		// Command validation happens upstream before reaching this point.
		cmdArgs := []string{"/C", "chcp 65001 >nul 2>&1 & " + command}
		cmdArgs = append(cmdArgs, args...)
		cmd = exec.Command("cmd.exe", cmdArgs...)
	} else {
		// Unix: use sh with the full command
		cmdStr := command
		if len(args) > 0 {
			cmdStr += " " + strings.Join(args, " ")
		}
		cmd = exec.Command("sh", "-c", cmdStr)
	}

	// Set working directory
	if workingDir != "" {
		cmd.Dir = workingDir
	}

	// Filter and set environment variables
	if len(env) > 0 {
		envList := make([]string, 0, len(env))
		for k, v := range env {
			envList = append(envList, fmt.Sprintf("%s=%s", k, v))
		}
		// Apply environment filtering
		envList = se.config.FilterEnvironment(envList)
		cmd.Env = envList
	}

	// Apply resource limits (Linux/Unix only)
	if runtime.GOOS != "windows" {
		se.applyResourceLimits(cmd)
	}

	return cmd, nil
}

// ValidateCommand validates a command against sandbox rules before execution
func (se *SandboxExecutor) ValidateCommand(command string, workingDir string) error {
	if !se.config.Enabled {
		return nil
	}

	// Parse command to get executable
	cmdParts := strings.Fields(command)
	if len(cmdParts) == 0 {
		return fmt.Errorf("empty command")
	}

	executable := cmdParts[0]

	// Validate executable
	if err := se.config.ValidateExecutable(executable); err != nil {
		return err
	}

	// Validate working directory
	if workingDir != "" {
		if err := se.config.ValidatePath(workingDir); err != nil {
			return fmt.Errorf("工作目录不允许: %w", err)
		}
	}

	// Check for path references in command arguments
	// This is a basic check; more sophisticated parsing might be needed
	for _, part := range cmdParts[1:] {
		if strings.HasPrefix(part, "/") || strings.HasPrefix(part, "./") || strings.HasPrefix(part, "../") {
			// Looks like a path, validate it
			if err := se.config.ValidatePath(part); err != nil {
				// Only warn, don't fail - might be a false positive
				// Could be improved with better parsing
			}
		}
	}

	return nil
}

// GetConfig returns the current sandbox configuration
func (se *SandboxExecutor) GetConfig() *SandboxConfig {
	return se.config
}

// UpdateConfig updates the sandbox configuration
func (se *SandboxExecutor) UpdateConfig(config *SandboxConfig) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid sandbox config: %w", err)
	}
	se.config = config
	return nil
}
