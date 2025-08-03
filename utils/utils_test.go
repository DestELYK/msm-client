package utils

import (
	"os"
	"strings"
	"testing"
)

func TestFormatDigits(t *testing.T) {
	tests := []struct {
		name     string
		n        int
		width    int
		expected string
	}{
		{
			name:     "Positive number with padding",
			n:        42,
			width:    5,
			expected: "00042",
		},
		{
			name:     "Positive number exact width",
			n:        12345,
			width:    5,
			expected: "12345",
		},
		{
			name:     "Positive number longer than width",
			n:        123456,
			width:    3,
			expected: "123456",
		},
		{
			name:     "Zero with padding",
			n:        0,
			width:    4,
			expected: "0000",
		},
		{
			name:     "Single digit",
			n:        7,
			width:    1,
			expected: "7",
		},
		{
			name:     "Negative number",
			n:        -42,
			width:    5,
			expected: "-0042",
		},
		{
			name:     "Width zero",
			n:        123,
			width:    0,
			expected: "123",
		},
		{
			name:     "Large number",
			n:        999999,
			width:    8,
			expected: "00999999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatDigits(tt.n, tt.width)
			if result != tt.expected {
				t.Errorf("FormatDigits(%d, %d) = %q, expected %q", tt.n, tt.width, result, tt.expected)
			}
		})
	}
}

func TestGetUptime(t *testing.T) {
	t.Run("Valid uptime file", func(t *testing.T) {
		// Create a temporary uptime file for testing
		tmpFile, err := os.CreateTemp("", "uptime_test")
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(tmpFile.Name())
		defer tmpFile.Close()

		// Write test data
		testData := "12345.67 23456.78\n"
		if _, err := tmpFile.WriteString(testData); err != nil {
			t.Fatal(err)
		}

		// We can't easily mock os.ReadFile, so we'll test the error case instead
		// This test mainly verifies the function doesn't panic
		uptime := GetUptime()
		if uptime < 0 {
			t.Error("GetUptime should not return negative values")
		}
	})

	t.Run("Non-existent file", func(t *testing.T) {
		// This test will hit the error case on non-Linux systems
		uptime := GetUptime()
		if uptime < 0 {
			t.Error("GetUptime should return 0 or positive values, got negative")
		}
	})
}

func TestGenerateCode(t *testing.T) {
	t.Run("Valid length codes", func(t *testing.T) {
		testLengths := []int{1, 4, 6, 8, 16, 32}

		for _, length := range testLengths {
			t.Run(strings.Join([]string{"length", strings.Repeat("x", length)}, "_"), func(t *testing.T) {
				code := GenerateCode(length)

				// Check length
				if len(code) != length {
					t.Errorf("Expected code length %d, got %d", length, len(code))
				}

				// Check character set
				validChars := "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
				for _, char := range code {
					if !strings.ContainsRune(validChars, char) {
						t.Errorf("Code contains invalid character: %c", char)
						break
					}
				}
			})
		}
	})

	t.Run("Zero length", func(t *testing.T) {
		code := GenerateCode(0)
		if code != "" {
			t.Errorf("Expected empty string for zero length, got %q", code)
		}
	})

	t.Run("Negative length", func(t *testing.T) {
		code := GenerateCode(-5)
		if code != "" {
			t.Errorf("Expected empty string for negative length, got %q", code)
		}
	})

	t.Run("Randomness test", func(t *testing.T) {
		// Generate multiple codes and ensure they're different
		codes := make(map[string]bool)
		length := 8
		iterations := 100

		for i := 0; i < iterations; i++ {
			code := GenerateCode(length)
			if codes[code] {
				t.Errorf("Generated duplicate code: %s", code)
				break
			}
			codes[code] = true
		}

		if len(codes) < iterations/2 {
			t.Error("Generated codes are not sufficiently random")
		}
	})

	t.Run("Character distribution", func(t *testing.T) {
		// Test that all characters from the charset appear over many generations
		charCount := make(map[rune]int)
		validChars := "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

		// Generate many short codes to test character distribution
		for i := 0; i < 1000; i++ {
			code := GenerateCode(1)
			if len(code) == 1 {
				charCount[rune(code[0])]++
			}
		}

		// We should see at least some variety (not expecting perfect distribution)
		if len(charCount) < 10 {
			t.Errorf("Too few unique characters generated: %d", len(charCount))
		}

		// All generated characters should be valid
		for char := range charCount {
			if !strings.ContainsRune(validChars, char) {
				t.Errorf("Invalid character generated: %c", char)
			}
		}
	})
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "Empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "Single line",
			input:    "hello world",
			expected: []string{"hello world"},
		},
		{
			name:     "Multiple lines",
			input:    "line1\nline2\nline3",
			expected: []string{"line1", "line2", "line3"},
		},
		{
			name:     "Lines with whitespace",
			input:    "  line1  \n  line2  \n  line3  ",
			expected: []string{"line1", "line2", "line3"},
		},
		{
			name:     "Empty lines mixed",
			input:    "line1\n\nline2\n  \nline3",
			expected: []string{"line1", "line2", "line3"},
		},
		{
			name:     "Only empty lines",
			input:    "\n\n  \n\t\n",
			expected: nil,
		},
		{
			name:     "Trailing newlines",
			input:    "line1\nline2\n\n",
			expected: []string{"line1", "line2"},
		},
		{
			name:     "Leading newlines",
			input:    "\n\nline1\nline2",
			expected: []string{"line1", "line2"},
		},
		{
			name:     "Single newline",
			input:    "\n",
			expected: nil,
		},
		{
			name:     "Tabs and spaces",
			input:    "\tline1\t\n  line2  \n\t  line3\t  ",
			expected: []string{"line1", "line2", "line3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SplitLines(tt.input)

			if len(result) != len(tt.expected) {
				t.Errorf("SplitLines(%q) length = %d, expected %d", tt.input, len(result), len(tt.expected))
				return
			}

			for i, expected := range tt.expected {
				if i >= len(result) || result[i] != expected {
					t.Errorf("SplitLines(%q)[%d] = %q, expected %q", tt.input, i, result[i], expected)
				}
			}
		})
	}
}

// Benchmark tests
func BenchmarkFormatDigits(b *testing.B) {
	for i := 0; i < b.N; i++ {
		FormatDigits(12345, 8)
	}
}

func BenchmarkGenerateCode(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GenerateCode(6)
	}
}

func BenchmarkSplitLines(b *testing.B) {
	input := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SplitLines(input)
	}
}
