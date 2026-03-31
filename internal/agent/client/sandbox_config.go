package client

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// windowsBuiltinCommands defines Windows cmd.exe built-in commands that do not exist as standalone executables
// These commands are built into cmd.exe and should be allowed when running on Windows
var windowsBuiltinCommands = map[string]bool{
	"assoc":    true,
	"break":    true,
	"call":     true,
	"cd":       true,
	"chdir":    true,
	"cls":      true,
	"color":    true,
	"copy":     true,
	"date":     true,
	"del":      true,
	"dir":      true,
	"echo":     true,
	"endlocal": true,
	"erase":    true,
	"exit":     true,
	"for":      true,
	"ftype":    true,
	"goto":     true,
	"if":       true,
	"md":       true,
	"mkdir":    true,
	"mklink":   true,
	"move":     true,
	"path":     true,
	"pause":    true,
	"popd":     true,
	"prompt":   true,
	"pushd":    true,
	"rd":       true,
	"rem":      true,
	"ren":      true,
	"rename":   true,
	"rmdir":    true,
	"set":      true,
	"setlocal": true,
	"shift":    true,
	"start":    true,
	"time":     true,
	"title":    true,
	"type":     true,
	"ver":      true,
	"verify":   true,
	"vol":      true,
}

// SandboxConfig defines the security sandbox configuration for command execution
type SandboxConfig struct {
	// Enabled indicates if sandbox is enabled
	Enabled bool

	// AllowedPaths defines the whitelist of paths that commands can access
	// Empty list means all paths are allowed (not recommended)
	AllowedPaths []string

	// DeniedPaths defines paths that are explicitly denied
	// Takes priority over AllowedPaths
	DeniedPaths []string

	// AllowedExecutables defines the whitelist of executable paths
	// Empty list means all executables are allowed
	AllowedExecutables []string

	// MaxCPUPercent limits CPU usage (0-100, 0 means no limit)
	MaxCPUPercent int

	// MaxMemoryMB limits memory usage in MB (0 means no limit)
	MaxMemoryMB int

	// MaxExecutionTime limits command execution time
	MaxExecutionTime time.Duration

	// RestrictNetwork indicates if network access should be restricted
	RestrictNetwork bool

	// ReadOnlyPaths defines paths that should be mounted as read-only
	ReadOnlyPaths []string

	// AllowEnvironmentVariables defines which environment variables are allowed
	AllowEnvironmentVariables []string
}

// DefaultSandboxConfig returns a secure default sandbox configuration
func DefaultSandboxConfig() *SandboxConfig {
	return &SandboxConfig{
		Enabled:           true,
		AllowedPaths:      []string{"/tmp", "/var/tmp", "/home"},
		DeniedPaths:       []string{"/etc/shadow", "/etc/passwd", "/root/.ssh", "/.ssh"},
		AllowedExecutables: []string{
			"/bin", "/usr/bin", "/usr/local/bin",
			"/sbin", "/usr/sbin",
		},
		MaxCPUPercent:     50,
		MaxMemoryMB:       512,
		MaxExecutionTime:  5 * time.Minute,
		RestrictNetwork:   false,
		ReadOnlyPaths:     []string{"/etc", "/usr", "/bin", "/sbin", "/lib", "/lib64"},
		AllowEnvironmentVariables: []string{
			"PATH", "HOME", "USER", "SHELL", "TERM",
			"LANG", "LC_*", "TZ",
		},
	}
}

// ValidatePath checks if a path is allowed by the sandbox configuration
func (sc *SandboxConfig) ValidatePath(path string) error {
	if !sc.Enabled {
		return nil
	}

	// Resolve to absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	// Clean the path to prevent directory traversal
	absPath = filepath.Clean(absPath)

	// Check denied paths first (takes priority)
	for _, denied := range sc.DeniedPaths {
		deniedAbs, _ := filepath.Abs(denied)
		deniedAbs = filepath.Clean(deniedAbs)
		// Use filepath.Rel to check if absPath is under deniedAbs
		rel, err := filepath.Rel(deniedAbs, absPath)
		if err == nil && !strings.HasPrefix(rel, "..") {
			return fmt.Errorf("访问被拒绝: 路径 %s 在黑名单中", absPath)
		}
	}

	// If allowed paths is empty, allow all (except denied)
	if len(sc.AllowedPaths) == 0 {
		return nil
	}

	// Check if path is in allowed list
	for _, allowed := range sc.AllowedPaths {
		allowedAbs, _ := filepath.Abs(allowed)
		allowedAbs = filepath.Clean(allowedAbs)
		// Use filepath.Rel to check if absPath is under allowedAbs
		rel, err := filepath.Rel(allowedAbs, absPath)
		if err == nil && !strings.HasPrefix(rel, "..") {
			return nil
		}
	}

	return fmt.Errorf("访问被拒绝: 路径 %s 不在白名单中", absPath)
}

// ValidateExecutable checks if an executable is allowed
func (sc *SandboxConfig) ValidateExecutable(executable string) error {
	if !sc.Enabled {
		return nil
	}

	// If allowed executables is empty, allow all
	if len(sc.AllowedExecutables) == 0 {
		return nil
	}

	// On Windows, allow built-in shell commands (like dir, cd, copy, etc.)
	// These commands are built into cmd.exe and don't exist as standalone executables
	if runtime.GOOS == "windows" && isWindowsBuiltinCommand(executable) {
		return nil
	}

	// Resolve executable path
	execPath := executable
	if !filepath.IsAbs(executable) {
		// Try to find in PATH
		foundPath, err := findExecutableInPath(executable, sc.AllowedExecutables)
		if err != nil {
			return fmt.Errorf("可执行文件不在允许的路径中: %s", executable)
		}
		execPath = foundPath
	}

	// Check if executable is in allowed directories
	for _, allowedDir := range sc.AllowedExecutables {
		if strings.HasPrefix(execPath, allowedDir) {
			return nil
		}
	}

	return fmt.Errorf("可执行文件不在允许的路径中: %s", execPath)
}

// isWindowsBuiltinCommand checks if a command is a Windows cmd.exe built-in command
func isWindowsBuiltinCommand(command string) bool {
	// Normalize to lowercase for case-insensitive comparison (Windows commands are case-insensitive)
	return windowsBuiltinCommands[strings.ToLower(command)]
}

// findExecutableInPath searches for an executable in allowed paths
func findExecutableInPath(name string, allowedPaths []string) (string, error) {
	for _, dir := range allowedPaths {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			// Check if file is executable
			if info.Mode()&0111 != 0 {
				return path, nil
			}
		}
	}
	return "", fmt.Errorf("executable not found in allowed paths: %s", name)
}

// FilterEnvironment filters environment variables based on allowed list
func (sc *SandboxConfig) FilterEnvironment(env []string) []string {
	if !sc.Enabled || len(sc.AllowEnvironmentVariables) == 0 {
		return env
	}

	filtered := make([]string, 0)
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) < 2 {
			continue
		}

		envName := parts[0]
		allowed := false

		for _, allowedPattern := range sc.AllowEnvironmentVariables {
			// Support wildcard matching (e.g., LC_*)
			if strings.HasSuffix(allowedPattern, "*") {
				prefix := strings.TrimSuffix(allowedPattern, "*")
				if strings.HasPrefix(envName, prefix) {
					allowed = true
					break
				}
			} else if envName == allowedPattern {
				allowed = true
				break
			}
		}

		if allowed {
			filtered = append(filtered, e)
		}
	}

	return filtered
}

// Validate checks if the sandbox configuration is valid
func (sc *SandboxConfig) Validate() error {
	if !sc.Enabled {
		return nil
	}

	if sc.MaxCPUPercent < 0 || sc.MaxCPUPercent > 100 {
		return fmt.Errorf("invalid MaxCPUPercent: must be between 0 and 100")
	}

	if sc.MaxMemoryMB < 0 {
		return fmt.Errorf("invalid MaxMemoryMB: must be non-negative")
	}

	if sc.MaxExecutionTime < 0 {
		return fmt.Errorf("invalid MaxExecutionTime: must be non-negative")
	}

	return nil
}
