package client

import (
	"testing"
)

func TestSanitizeUTF8(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Valid UTF-8 string",
			input:    "Hello, World!",
			expected: "Hello, World!",
		},
		{
			name:     "Valid UTF-8 with unicode",
			input:    "你好世界",
			expected: "你好世界",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "Invalid UTF-8 bytes",
			input:    "Hello\xff\xfeWorld",
			expected: "Hello��World",
		},
		{
			name:     "Mix of valid and invalid UTF-8",
			input:    "Valid text \x80\x81 more text",
			expected: "Valid text �� more text",
		},
		{
			name:     "Only invalid UTF-8 bytes",
			input:    "\xff\xfe\xfd",
			expected: "���",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeUTF8String(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeUTF8String(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
