//go:build windows
// +build windows

package client

import (
	"os/exec"
)

// applyResourceLimits applies resource limits to the command (Windows - no-op)
func (se *SandboxExecutor) applyResourceLimits(cmd *exec.Cmd) {
	// Windows does not support process groups in the same way as Unix
	// Resource limits would need to be implemented using Job Objects
	// For now, this is a no-op on Windows
}
