package client

import (
	"bytes"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

func TestMultiEncodingReader_UTF8(t *testing.T) {
	// Test data that's already UTF-8
	input := "Hello, 世界! UTF-8 text"
	reader := newMultiEncodingReader(strings.NewReader(input))
	
	buf := make([]byte, 1024)
	n, err := reader.Read(buf)
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}
	
	result := string(buf[:n])
	if result != input {
		t.Errorf("Expected %q, got %q", input, result)
	}
}

func TestMultiEncodingReader_GBK(t *testing.T) {
	// Skip on non-Windows since multiEncodingReader only works on Windows
	if runtime.GOOS != "windows" {
		t.Skip("Skipping multiEncodingReader test on non-Windows")
	}
	
	// Test GBK encoded Chinese text
	originalText := "你好，世界！这是中文测试。"
	
	// Encode to GBK
	encoder := simplifiedchinese.GBK.NewEncoder()
	var buf bytes.Buffer
	writer := transform.NewWriter(&buf, encoder)
	_, err := writer.Write([]byte(originalText))
	if err != nil {
		t.Fatalf("Failed to encode to GBK: %v", err)
	}
	writer.Close()
	gbkBytes := buf.Bytes()
	
	// Test the multi-encoding reader
	reader := newMultiEncodingReader(bytes.NewReader(gbkBytes))
	readBuf := make([]byte, 1024)
	n, err := reader.Read(readBuf)
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}
	
	result := string(readBuf[:n])
	if result != originalText {
		t.Errorf("Expected %q, got %q", originalText, result)
	}
}

func TestMultiEncodingReader_Windows1252(t *testing.T) {
	// Test Windows-1252 encoded text (Western European)
	originalText := "Hello, World!"
	
	// Encode to Windows-1252
	encoder := charmap.Windows1252.NewEncoder()
	var buf bytes.Buffer
	writer := transform.NewWriter(&buf, encoder)
	_, err := writer.Write([]byte(originalText))
	if err != nil {
		t.Fatalf("Failed to encode to Windows-1252: %v", err)
	}
	writer.Close()
	win1252Bytes := buf.Bytes()
	
	// Test the multi-encoding reader
	reader := newMultiEncodingReader(bytes.NewReader(win1252Bytes))
	readBuf := make([]byte, 1024)
	n, err := reader.Read(readBuf)
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}
	
	result := string(readBuf[:n])
	if result != originalText {
		t.Errorf("Expected %q, got %q", originalText, result)
	}
}

func TestEncodingScanner_UTF8(t *testing.T) {
	input := "Line 1\nLine 2\n你好世界\nLine 4"
	scanner := NewEncodingScanner(strings.NewReader(input))
	
	expectedLines := []string{"Line 1", "Line 2", "你好世界", "Line 4"}
	var lines []string
	
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	
	if err := scanner.Err(); err != nil {
		t.Fatalf("Scanner error: %v", err)
	}
	
	if len(lines) != len(expectedLines) {
		t.Fatalf("Expected %d lines, got %d", len(expectedLines), len(lines))
	}
	
	for i, expected := range expectedLines {
		if lines[i] != expected {
			t.Errorf("Line %d: expected %q, got %q", i+1, expected, lines[i])
		}
	}
}

func TestEncodingScanner_GBK(t *testing.T) {
	// Test scanning GBK encoded text line by line
	originalLines := []string{
		"第一行",
		"第二行",
		"你好世界",
		"测试完成",
	}
	
	// Encode each line to GBK and join with newlines
	encoder := simplifiedchinese.GBK.NewEncoder()
	var buf bytes.Buffer
	writer := transform.NewWriter(&buf, encoder)
	
	for i, line := range originalLines {
		_, err := writer.Write([]byte(line))
		if err != nil {
			t.Fatalf("Failed to encode line %d to GBK: %v", i+1, err)
		}
		if i < len(originalLines)-1 {
			writer.Write([]byte{'\n'})
		}
	}
	writer.Close()
	gbkBytes := buf.Bytes()
	
	// Scan the GBK encoded text
	scanner := NewEncodingScanner(bytes.NewReader(gbkBytes))
	var lines []string
	
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	
	if err := scanner.Err(); err != nil {
		t.Fatalf("Scanner error: %v", err)
	}
	
	if len(lines) != len(originalLines) {
		t.Fatalf("Expected %d lines, got %d", len(originalLines), len(lines))
	}
	
	for i, expected := range originalLines {
		if lines[i] != expected {
			t.Errorf("Line %d: expected %q, got %q", i+1, expected, lines[i])
		}
	}
}

func TestTryConvertToUTF8_MultipleEncodings(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		encoding encoding.Encoding
	}{
		{
			name:     "GBK Chinese",
			input:    "中文测试",
			encoding: simplifiedchinese.GBK,
		},
		{
			name:     "Windows-1252 Western",
			input:    "Hello World",
			encoding: charmap.Windows1252,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode to the target encoding
			encoder := tt.encoding.NewEncoder()
			var buf bytes.Buffer
			writer := transform.NewWriter(&buf, encoder)
			_, err := writer.Write([]byte(tt.input))
			if err != nil {
				t.Fatalf("Failed to encode: %v", err)
			}
			writer.Close()
			encoded := buf.Bytes()
			
			// Try to convert back to UTF-8
			result := tryConvertToUTF8(encoded)
			resultStr := string(result)
			
			if resultStr != tt.input {
				t.Errorf("Expected %q, got %q", tt.input, resultStr)
			}
		})
	}
}

func TestTryConvertToUTF8_AlreadyUTF8(t *testing.T) {
	input := "Hello, 世界! Already UTF-8"
	result := tryConvertToUTF8([]byte(input))
	
	if string(result) != input {
		t.Errorf("Expected %q, got %q", input, string(result))
	}
}

func TestSanitizeUTF8String_Valid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "ASCII only",
			input: "Hello, World!",
		},
		{
			name:  "UTF-8 Chinese",
			input: "你好世界",
		},
		{
			name:  "UTF-8 Japanese",
			input: "こんにちは",
		},
		{
			name:  "UTF-8 Emoji",
			input: "Hello 👋 World 🌍",
		},
		{
			name:  "Mixed UTF-8",
			input: "Hello, 世界! こんにちは 👋",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeUTF8String(tt.input)
			if result != tt.input {
				t.Errorf("Expected %q, got %q", tt.input, result)
			}
		})
	}
}

func TestSanitizeUTF8String_Invalid(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Single invalid byte",
			input:    "Hello\xffWorld",
			expected: "Hello�World",
		},
		{
			name:     "Multiple invalid bytes",
			input:    "Test\xff\xfe\xfdEnd",
			expected: "Test���End",
		},
		{
			name:     "Invalid at start",
			input:    "\xff\xfeStart",
			expected: "��Start",
		},
		{
			name:     "Invalid at end",
			input:    "End\xff\xfe",
			expected: "End��",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeUTF8String(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func BenchmarkMultiEncodingReader_UTF8(b *testing.B) {
	input := "Hello, World! 你好世界"
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		reader := newMultiEncodingReader(strings.NewReader(input))
		buf := make([]byte, 1024)
		_, _ = reader.Read(buf)
	}
}

func BenchmarkMultiEncodingReader_GBK(b *testing.B) {
	originalText := "你好，世界！这是中文测试。"
	encoder := simplifiedchinese.GBK.NewEncoder()
	var buf bytes.Buffer
	writer := transform.NewWriter(&buf, encoder)
	writer.Write([]byte(originalText))
	writer.Close()
	gbkBytes := buf.Bytes()
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		reader := newMultiEncodingReader(bytes.NewReader(gbkBytes))
		readBuf := make([]byte, 1024)
		_, _ = reader.Read(readBuf)
	}
}
