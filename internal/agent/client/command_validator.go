package client

import (
	"errors"
	"regexp"
	"strings"
)

// CommandValidator validates commands before execution
type CommandValidator struct {
	// DangerousPatterns are patterns that should trigger warnings
	dangerousPatterns []string
}

// NewCommandValidator creates a new command validator
func NewCommandValidator() *CommandValidator {
	return &CommandValidator{
		dangerousPatterns: []string{
			`\brm\s+-[rfRF]*[rfRF]`, // rm with -r or -f
			`\bmkfs\b`,              // mkfs
			`\bdd\s+.*of=/dev/`,     // dd to device
			`\bfdisk\b`,             // fdisk
			`\bparted\b`,            // parted
			`\bshutdown\b`,          // shutdown
			`\breboot\b`,            // reboot
			`\bpoweroff\b`,          // poweroff
			`:\(\)\{.*:\|:`,         // fork bomb
			`\biptables\s+-F`,       // iptables flush
		},
	}
}

// ValidateCommand checks if a command is safe to execute
// Returns error if command appears dangerous, warning message otherwise
func (v *CommandValidator) ValidateCommand(command string) error {
	if command == "" {
		return errors.New("empty command")
	}

	// Check for extremely dangerous patterns
	criticalPatterns := []string{
		`\brm\s+(-[rfRF]*[rfRF][rfRF]*\s+)?/\s*$`,     // rm -rf /
		`\bmkfs\.(ext[234]|xfs|btrfs)\s+/dev/(sd|hd)`, // mkfs on physical disk
		`\bdd\s+.*of=/dev/(sd|hd|nvme|vd)[a-z]`,       // dd to disk
		`:\(\)\{.*:\|:.*\};:`,                          // fork bomb exact pattern
	}

	for _, pattern := range criticalPatterns {
		if matched, _ := regexp.MatchString(pattern, command); matched {
			return errors.New("命令被安全策略阻止: 此命令可能造成严重系统损坏")
		}
	}

	// Check for generally dangerous patterns (warnings only, not blocked)
	for _, pattern := range v.dangerousPatterns {
		if matched, _ := regexp.MatchString(pattern, command); matched {
			// Log warning but don't block
			// In production, this might send an alert or require confirmation
			break
		}
	}

	return nil
}

// GetRiskLevel returns the risk level of a command
func (v *CommandValidator) GetRiskLevel(command string) string {
	// Critical patterns
	criticalPatterns := []string{
		`\brm\s+(-[rfRF]*[rfRF][rfRF]*\s+)?/\s*$`,
		`\bmkfs\b`,
		`\bdd\s+.*of=/dev/(sd|hd)`,
		`\bfdisk\b`,
		`\bparted\b`,
		`\bshutdown\b`,
		`\breboot\b`,
		`:\(\)\{.*:\|:`,
	}

	for _, pattern := range criticalPatterns {
		if matched, _ := regexp.MatchString(pattern, command); matched {
			return "critical"
		}
	}

	// High risk patterns
	highRiskPatterns := []string{
		`\brm\s+-[rfRF]`,
		`\bchmod\s+777`,
		`\bchown\s+root`,
		`\bsudo\b`,
	}

	for _, pattern := range highRiskPatterns {
		if matched, _ := regexp.MatchString(pattern, command); matched {
			return "high"
		}
	}

	// Medium risk patterns
	mediumRiskPatterns := []string{
		`\brm\b`,
		`\bmv\b.*\s+/`,
		`\bchmod\b`,
		`\bsystemctl\s+(stop|restart)`,
	}

	for _, pattern := range mediumRiskPatterns {
		if matched, _ := regexp.MatchString(pattern, command); matched {
			return "medium"
		}
	}

	return "low"
}

// ShouldWarn returns true if command should trigger a warning
func (v *CommandValidator) ShouldWarn(command string) bool {
	riskLevel := v.GetRiskLevel(command)
	return riskLevel == "high" || riskLevel == "critical"
}

// IsSafeCommand checks if command contains only safe characters
func IsSafeCommand(command string) bool {
	// Check for suspicious sequences
	suspicious := []string{
		"&&",      // command chaining
		"||",      // command chaining
		";",       // command separator
		"|",       // pipe
		">",       // redirect
		"<",       // redirect
		"`",       // command substitution
		"$(",      // command substitution
		"${",      // variable expansion
		"\n",      // newline injection
		"\r",      // carriage return injection
	}

	for _, pattern := range suspicious {
		if strings.Contains(command, pattern) {
			// These aren't necessarily dangerous, just warrant extra scrutiny
			// The server should do final validation
			return false
		}
	}

	return true
}
