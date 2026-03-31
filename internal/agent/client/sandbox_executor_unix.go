//go:build !windows
// +build !windows

package client

import (
	"os/exec"
	"syscall"
)

// applyResourceLimits applies resource limits to the command (Linux/Unix only)
func (se *SandboxExecutor) applyResourceLimits(cmd *exec.Cmd) {
	// Set process group to allow killing all child processes
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// Note: CPU and Memory limits require cgroups or other OS-specific mechanisms
	// For now, we only set the execution timeout via context
	// Full cgroup integration would require root privileges or systemd integration
}
