package utils

import (
	"crypto/rand"
	"fmt"
	"log"
	"os"
	"strings"
)

// FormatDigits formats an integer with zero-padding to the specified width.
// For example, FormatDigits(42, 5) returns "00042".
func FormatDigits(n, width int) string {
	return fmt.Sprintf("%0*d", width, n)
}

// GetUptime returns the system uptime in seconds.
// Returns 0 if the uptime cannot be determined (e.g., on non-Linux systems).
func GetUptime() int64 {
	// This function is Linux-specific, reading from /proc/uptime
	uptime, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0 // Return 0 if we can't read uptime (e.g., not on Linux)
	}

	// Parse the uptime value (first number in the file)
	var seconds float64
	if _, err := fmt.Sscanf(string(uptime), "%f", &seconds); err != nil {
		return 0 // Return 0 if parsing fails
	}

	return int64(seconds)
}

// GenerateCode generates a cryptographically secure random pairing code.
// The code consists of uppercase letters and digits (A-Z, 0-9).
// Returns a code of the specified length, or a fallback code if crypto/rand fails.
func GenerateCode(codeLength int) string {
	if codeLength <= 0 {
		return ""
	}

	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	const charsetLen = len(charset)

	result := make([]byte, codeLength)

	// Generate random bytes
	randomBytes := make([]byte, codeLength)
	if _, err := rand.Read(randomBytes); err != nil {
		// Fallback to a deterministic pattern if crypto/rand fails
		log.Printf("Warning: crypto/rand failed, using fallback pattern: %v", err)
		for i := range result {
			result[i] = charset[i%charsetLen]
		}
		return string(result)
	}

	// Map random bytes to charset
	for i, b := range randomBytes {
		result[i] = charset[int(b)%charsetLen]
	}

	return string(result)
}

// SplitLines splits a string by newlines and returns non-empty trimmed lines.
// Empty lines and lines containing only whitespace are filtered out.
func SplitLines(s string) []string {
	if s == "" {
		return nil
	}

	lines := strings.Split(s, "\n")
	result := make([]string, 0, len(lines)) // Pre-allocate with capacity

	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			result = append(result, trimmed)
		}
	}

	return result
}
