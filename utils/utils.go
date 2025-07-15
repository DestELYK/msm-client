package utils

import (
	"os"
	"strconv"
)


func FormatDigits(n, width int) string {
	return PadLeft("0000", width, n)
}

func PadLeft(zeroes string, width int, n int) string {
	s := zeroes + strconv.Itoa(n)
	return s[len(s)-width:]
}

func WriteFile(filename string, data []byte) error {
	return os.WriteFile(filename, data, 0600)
}

func ReadFile(filename string) ([]byte, error) {
	data, err := os.ReadFile(filename)
	if os.IsNotExist(err) {
		return nil, nil // File does not exist, return nil
	}
	if err != nil {
		return nil, err // Other errors
	}
	return data, nil
}

func DeleteFile(filename string) error {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return nil // File does not exist, nothing to delete
	}
	return os.Remove(filename)
}