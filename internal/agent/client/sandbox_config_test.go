package client

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultSandboxConfig(t *testing.T) {
	config := DefaultSandboxConfig()

	if !config.Enabled {
		t.Error("Default sandbox should be enabled")
	}

	if len(config.AllowedPaths) == 0 {
		t.Error("Default sandbox should have allowed paths")
	}

	if len(config.DeniedPaths) == 0 {
		t.Error("Default sandbox should have denied paths")
	}

	if len(config.AllowedExecutables) == 0 {
		t.Error("Default sandbox should have allowed executables")
	}
}

func TestSandboxConfig_ValidatePath(t *testing.T) {
	config := DefaultSandboxConfig()

	tests := []struct {
		name      string
		path      string
		shouldErr bool
	}{
		{
			name:      "Allowed path /tmp",
			path:      "/tmp/test.txt",
			shouldErr: false,
		},
		{
			name:      "Allowed path /home",
			path:      "/home/user/file.txt",
			shouldErr: false,
		},
		{
			name:      "Denied path /etc/shadow",
			path:      "/etc/shadow",
			shouldErr: true,
		},
		{
			name:      "Denied path /etc/passwd",
			path:      "/etc/passwd",
			shouldErr: true,
		},
		{
			name:      "Not allowed path /root",
			path:      "/root/secret.txt",
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := config.ValidatePath(tt.path)
			if (err != nil) != tt.shouldErr {
				t.Errorf("ValidatePath() error = %v, shouldErr %v", err, tt.shouldErr)
			}
		})
	}
}

func TestSandboxConfig_ValidateExecutable(t *testing.T) {
	config := DefaultSandboxConfig()

	tests := []struct {
		name       string
		executable string
		shouldErr  bool
	}{
		{
			name:       "Allowed executable /bin/ls",
			executable: "/bin/ls",
			shouldErr:  false,
		},
		{
			name:       "Allowed executable /usr/bin/cat",
			executable: "/usr/bin/cat",
			shouldErr:  false,
		},
		{
			name:       "Not allowed executable /root/malicious",
			executable: "/root/malicious",
			shouldErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := config.ValidateExecutable(tt.executable)
			if (err != nil) != tt.shouldErr {
				t.Errorf("ValidateExecutable() error = %v, shouldErr %v", err, tt.shouldErr)
			}
		})
	}
}

func TestSandboxConfig_FilterEnvironment(t *testing.T) {
	config := DefaultSandboxConfig()

	env := []string{
		"PATH=/usr/bin",
		"HOME=/home/user",
		"SECRET_KEY=should_be_filtered",
		"LANG=en_US.UTF-8",
		"LC_ALL=en_US.UTF-8",
	}

	filtered := config.FilterEnvironment(env)

	// Check that allowed variables are present
	hasPath := false
	hasHome := false
	hasLang := false
	hasSecret := false

	for _, e := range filtered {
		if e == "PATH=/usr/bin" {
			hasPath = true
		}
		if e == "HOME=/home/user" {
			hasHome = true
		}
		if e == "LANG=en_US.UTF-8" {
			hasLang = true
		}
		if e == "SECRET_KEY=should_be_filtered" {
			hasSecret = true
		}
	}

	if !hasPath {
		t.Error("PATH should be in filtered environment")
	}
	if !hasHome {
		t.Error("HOME should be in filtered environment")
	}
	if !hasLang {
		t.Error("LANG should be in filtered environment")
	}
	if hasSecret {
		t.Error("SECRET_KEY should not be in filtered environment")
	}
}

func TestSandboxConfig_Validate(t *testing.T) {
	tests := []struct {
		name      string
		config    *SandboxConfig
		shouldErr bool
	}{
		{
			name:      "Valid default config",
			config:    DefaultSandboxConfig(),
			shouldErr: false,
		},
		{
			name: "Invalid MaxCPUPercent",
			config: &SandboxConfig{
				Enabled:       true,
				MaxCPUPercent: 150,
			},
			shouldErr: true,
		},
		{
			name: "Invalid MaxMemoryMB",
			config: &SandboxConfig{
				Enabled:     true,
				MaxMemoryMB: -100,
			},
			shouldErr: true,
		},
		{
			name: "Invalid MaxExecutionTime",
			config: &SandboxConfig{
				Enabled:          true,
				MaxExecutionTime: -10 * time.Second,
			},
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.shouldErr {
				t.Errorf("Validate() error = %v, shouldErr %v", err, tt.shouldErr)
			}
		})
	}
}

func TestSandboxConfig_DisabledSandbox(t *testing.T) {
	config := &SandboxConfig{
		Enabled: false,
	}

	// When disabled, all validations should pass
	if err := config.ValidatePath("/etc/shadow"); err != nil {
		t.Error("Disabled sandbox should allow all paths")
	}

	if err := config.ValidateExecutable("/root/malicious"); err != nil {
		t.Error("Disabled sandbox should allow all executables")
	}

	env := []string{"SECRET=value"}
	filtered := config.FilterEnvironment(env)
	if len(filtered) != len(env) {
		t.Error("Disabled sandbox should not filter environment")
	}
}

func TestIsWindowsBuiltinCommand(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		expected bool
	}{
		{
			name:     "dir command (lowercase)",
			command:  "dir",
			expected: true,
		},
		{
			name:     "DIR command (uppercase)",
			command:  "DIR",
			expected: true,
		},
		{
			name:     "Dir command (mixed case)",
			command:  "Dir",
			expected: true,
		},
		{
			name:     "cd command",
			command:  "cd",
			expected: true,
		},
		{
			name:     "copy command",
			command:  "copy",
			expected: true,
		},
		{
			name:     "del command",
			command:  "del",
			expected: true,
		},
		{
			name:     "type command",
			command:  "type",
			expected: true,
		},
		{
			name:     "echo command",
			command:  "echo",
			expected: true,
		},
		{
			name:     "set command",
			command:  "set",
			expected: true,
		},
		{
			name:     "mkdir command",
			command:  "mkdir",
			expected: true,
		},
		{
			name:     "rmdir command",
			command:  "rmdir",
			expected: true,
		},
		{
			name:     "notepad is not a builtin",
			command:  "notepad",
			expected: false,
		},
		{
			name:     "powershell is not a builtin",
			command:  "powershell",
			expected: false,
		},
		{
			name:     "custom command is not a builtin",
			command:  "mycustomcmd",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isWindowsBuiltinCommand(tt.command)
			if result != tt.expected {
				t.Errorf("isWindowsBuiltinCommand(%q) = %v, expected %v", tt.command, result, tt.expected)
			}
		})
	}
}

func TestIsWindowsBuiltinCommand_AllBuiltins(t *testing.T) {
	// Test that all commands in windowsBuiltinCommands are recognized
	for cmd := range windowsBuiltinCommands {
		t.Run(cmd, func(t *testing.T) {
			// Test lowercase
			if !isWindowsBuiltinCommand(cmd) {
				t.Errorf("isWindowsBuiltinCommand(%q) = false, expected true", cmd)
			}
			// Test uppercase (Windows commands are case-insensitive)
			upper := strings.ToUpper(cmd)
			if !isWindowsBuiltinCommand(upper) {
				t.Errorf("isWindowsBuiltinCommand(%q) = false, expected true", upper)
			}
		})
	}
}
