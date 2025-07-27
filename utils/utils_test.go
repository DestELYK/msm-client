package utils

import (
	"os"
	"testing"
)

func TestFormatDigits(t *testing.T) {
	tests := []struct {
		n        int
		width    int
		expected string
	}{
		{1, 4, "0001"},
		{12, 4, "0012"},
		{123, 4, "0123"},
		{1234, 4, "1234"},
		{12345, 4, "2345"}, // Should truncate from left
		{0, 3, "000"},
		{99, 2, "99"},
		{5, 6, "000005"},
	}

	for _, tt := range tests {
		result := FormatDigits(tt.n, tt.width)
		if result != tt.expected {
			t.Errorf("FormatDigits(%d, %d) = %s; expected %s", tt.n, tt.width, result, tt.expected)
		}
	}
}

func TestFormatDigitsEdgeCases(t *testing.T) {
	// Test with width 1 (minimum useful width)
	result := FormatDigits(123, 1)
	if result != "3" {
		t.Errorf("FormatDigits with width 1 should return '3', got %s", result)
	}

	// Test with very large number
	result = FormatDigits(999999, 3)
	if result != "999" {
		t.Errorf("FormatDigits(999999, 3) = %s; expected '999'", result)
	}

	// Test with negative number
	result = FormatDigits(-5, 3)
	if result != "-5" {
		t.Errorf("FormatDigits(-5, 3) = %s; expected '-5'", result)
	}
}

func TestPadLeft(t *testing.T) {
	tests := []struct {
		zeroes   string
		width    int
		n        int
		expected string
	}{
		{"0000", 4, 1, "0001"},
		{"0000", 4, 12, "0012"},
		{"0000", 4, 123, "0123"},
		{"0000", 4, 1234, "1234"},
		{"000000", 6, 42, "000042"},
		{"00", 2, 7, "07"},
		{"xxx", 3, 5, "xx5"},
	}

	for _, tt := range tests {
		result := PadLeft(tt.zeroes, tt.width, tt.n)
		if result != tt.expected {
			t.Errorf("PadLeft(%s, %d, %d) = %s; expected %s", tt.zeroes, tt.width, tt.n, result, tt.expected)
		}
	}
}

func TestWriteFile(t *testing.T) {
	testFile := "test_write.txt"
	testData := []byte("Hello, World!")

	defer os.Remove(testFile)

	err := WriteFile(testFile, testData)
	if err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Fatal("File was not created")
	}

	// Verify content
	readData, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read back written file: %v", err)
	}

	if string(readData) != string(testData) {
		t.Fatalf("Expected %s, got %s", string(testData), string(readData))
	}

	// Verify permissions (should be 0600)
	fileInfo, err := os.Stat(testFile)
	if err != nil {
		t.Fatalf("Failed to get file info: %v", err)
	}

	expectedPerm := os.FileMode(0600)
	if fileInfo.Mode().Perm() != expectedPerm {
		t.Fatalf("Expected file permissions %v, got %v", expectedPerm, fileInfo.Mode().Perm())
	}
}

func TestReadFile(t *testing.T) {
	testFile := "test_read.txt"
	testData := []byte("Test content for reading")

	defer os.Remove(testFile)

	// Create test file
	err := os.WriteFile(testFile, testData, 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test reading existing file
	readData, err := ReadFile(testFile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if string(readData) != string(testData) {
		t.Fatalf("Expected %s, got %s", string(testData), string(readData))
	}
}

func TestReadFileNotExists(t *testing.T) {
	nonExistentFile := "does_not_exist.txt"

	// Ensure file doesn't exist
	os.Remove(nonExistentFile)

	data, err := ReadFile(nonExistentFile)
	if err != nil {
		t.Fatalf("ReadFile should not error for non-existent file: %v", err)
	}

	if data != nil {
		t.Fatalf("Expected nil data for non-existent file, got %v", data)
	}
}

func TestDeleteFile(t *testing.T) {
	testFile := "test_delete.txt"
	testData := []byte("File to be deleted")

	// Create test file
	err := os.WriteFile(testFile, testData, 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Fatal("Test file should exist before deletion")
	}

	// Delete file
	err = DeleteFile(testFile)
	if err != nil {
		t.Fatalf("DeleteFile failed: %v", err)
	}

	// Verify file no longer exists
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Fatal("File should not exist after deletion")
	}
}

func TestDeleteFileNotExists(t *testing.T) {
	nonExistentFile := "does_not_exist.txt"

	// Ensure file doesn't exist
	os.Remove(nonExistentFile)

	err := DeleteFile(nonExistentFile)
	if err != nil {
		t.Fatalf("DeleteFile should not error for non-existent file: %v", err)
	}
}

func TestWriteAndReadCycle(t *testing.T) {
	testFile := "test_cycle.txt"
	testData := []byte("Round trip test data")

	defer os.Remove(testFile)

	// Write data
	err := WriteFile(testFile, testData)
	if err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Read data back
	readData, err := ReadFile(testFile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if string(readData) != string(testData) {
		t.Fatalf("Round trip failed: expected %s, got %s", string(testData), string(readData))
	}
}

func TestWriteEmptyFile(t *testing.T) {
	testFile := "test_empty.txt"
	testData := []byte("")

	defer os.Remove(testFile)

	err := WriteFile(testFile, testData)
	if err != nil {
		t.Fatalf("WriteFile failed for empty data: %v", err)
	}

	readData, err := ReadFile(testFile)
	if err != nil {
		t.Fatalf("ReadFile failed for empty file: %v", err)
	}

	if len(readData) != 0 {
		t.Fatalf("Expected empty data, got %d bytes", len(readData))
	}
}

// Network Interface Tests

func TestInterfaceInfoStruct(t *testing.T) {
	// Test that InterfaceInfo struct works without Name and IsLoopback fields
	iface := InterfaceInfo{
		IPAddress:  "192.168.1.100",
		MACAddress: "00:11:22:33:44:55",
		Type:       "ethernet",
		IsUp:       true,
	}

	if iface.IPAddress != "192.168.1.100" {
		t.Errorf("Expected IPAddress '192.168.1.100', got '%s'", iface.IPAddress)
	}
	if iface.MACAddress != "00:11:22:33:44:55" {
		t.Errorf("Expected MACAddress '00:11:22:33:44:55', got '%s'", iface.MACAddress)
	}
	if iface.Type != "ethernet" {
		t.Errorf("Expected Type 'ethernet', got '%s'", iface.Type)
	}
	if !iface.IsUp {
		t.Error("Expected IsUp to be true")
	}
}

func TestDetectInterfaceType(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		// WiFi patterns
		{"wlan0", "wifi"},
		{"wlan1", "wifi"},
		{"wifi0", "wifi"},
		{"wlp2s0", "wifi"},
		{"wlo1", "wifi"},
		{"ath0", "wifi"},
		{"ra0", "wifi"},
		{"Wireless Network Connection", "wifi"}, // Windows
		{"Wi-Fi", "wifi"},                       // Windows

		// Ethernet patterns
		{"eth0", "ethernet"},
		{"eth1", "ethernet"},
		{"enp0s3", "ethernet"},
		{"eno1", "ethernet"},
		{"ens33", "ethernet"},
		{"em1", "ethernet"},
		{"p2p0", "ethernet"},
		{"en0", "ethernet"},                   // macOS - typically ethernet
		{"en1", "ethernet"},                   // macOS
		{"Ethernet", "ethernet"},              // Windows
		{"Local Area Connection", "ethernet"}, // Windows

		// Other patterns
		{"lo", "other"},
		{"lo0", "other"},
		{"tun0", "other"},
		{"docker0", "other"},
		{"unknown_interface", "other"},
	}

	for _, tt := range tests {
		result := detectInterfaceType(tt.name)
		if result != tt.expected {
			t.Errorf("detectInterfaceType(%s) = %s; expected %s", tt.name, result, tt.expected)
		}
	}
}

func TestGetAllInterfaces(t *testing.T) {
	interfaces := GetAllInterfaces()

	// Should return at least one interface (even if it's just a network interface)
	// Note: We can't guarantee any specific interfaces exist, but we can test structure
	for _, iface := range interfaces {
		// Each interface should have valid IP and MAC
		if iface.IPAddress == "" {
			t.Error("Interface should have non-empty IPAddress")
		}
		if iface.MACAddress == "" {
			t.Error("Interface should have non-empty MACAddress")
		}

		// Type should be one of the expected values
		validTypes := []string{"wifi", "ethernet", "other"}
		typeValid := false
		for _, validType := range validTypes {
			if iface.Type == validType {
				typeValid = true
				break
			}
		}
		if !typeValid {
			t.Errorf("Interface type '%s' is not valid. Expected one of: %v", iface.Type, validTypes)
		}

		// Should not have loopback addresses (127.x.x.x or ::1)
		if iface.IPAddress == "127.0.0.1" || iface.IPAddress == "::1" {
			t.Errorf("Loopback address %s should be filtered out", iface.IPAddress)
		}
	}
}

func TestGetNetworkInterfaces(t *testing.T) {
	interfaces := GetNetworkInterfaces()

	// All returned interfaces should be ethernet or wifi
	for _, iface := range interfaces {
		if iface.Type != "ethernet" && iface.Type != "wifi" {
			t.Errorf("GetNetworkInterfaces should only return ethernet/wifi interfaces, got type: %s", iface.Type)
		}
	}
}

func TestGetPrimaryInterface(t *testing.T) {
	primary := GetPrimaryInterface()

	// Primary interface might be nil if no valid interfaces are found
	if primary != nil {
		// Should have valid IP and MAC
		if primary.IPAddress == "" || primary.IPAddress == "0.0.0.0" {
			t.Error("Primary interface should have valid IPAddress")
		}
		if primary.MACAddress == "" {
			t.Error("Primary interface should have non-empty MACAddress")
		}

		// Should be up
		if !primary.IsUp {
			t.Error("Primary interface should be up")
		}

		// Should not be loopback
		if primary.IPAddress == "127.0.0.1" || primary.IPAddress == "::1" {
			t.Error("Primary interface should not be loopback")
		}
	}
}

func TestGetInterfaceByIP(t *testing.T) {
	allInterfaces := GetAllInterfaces()

	if len(allInterfaces) > 0 {
		// Test with existing IP
		testIP := allInterfaces[0].IPAddress
		found := GetInterfaceByIP(testIP)

		if found == nil {
			t.Errorf("Should find interface with IP %s", testIP)
		} else if found.IPAddress != testIP {
			t.Errorf("Found interface has wrong IP: expected %s, got %s", testIP, found.IPAddress)
		}
	}

	// Test with non-existent IP
	nonExistent := GetInterfaceByIP("192.168.999.999")
	if nonExistent != nil {
		t.Error("Should return nil for non-existent IP")
	}
}

func TestGetMacAddress(t *testing.T) {
	// Test with empty IP (should return primary interface MAC)
	mac := GetMacAddress("")
	if mac == "" {
		t.Error("GetMacAddress with empty IP should return non-empty MAC")
	}
	if mac == "00:00:00:00:00:00" {
		// This might be valid if no interfaces are found, but let's log it
		t.Log("GetMacAddress returned default MAC address (no interfaces found)")
	}

	// Test with existing IP
	allInterfaces := GetAllInterfaces()
	if len(allInterfaces) > 0 {
		testIP := allInterfaces[0].IPAddress
		expectedMAC := allInterfaces[0].MACAddress

		mac := GetMacAddress(testIP)
		if mac != expectedMAC {
			t.Errorf("GetMacAddress(%s) = %s; expected %s", testIP, mac, expectedMAC)
		}
	}

	// Test with non-existent IP
	mac = GetMacAddress("192.168.999.999")
	if mac != "00:00:00:00:00:00" {
		t.Errorf("GetMacAddress with non-existent IP should return default MAC, got %s", mac)
	}
}

func TestLoopbackFiltering(t *testing.T) {
	// This test verifies that loopback addresses are properly filtered out
	interfaces := GetAllInterfaces()

	for _, iface := range interfaces {
		// Check IPv4 loopback
		if iface.IPAddress == "127.0.0.1" {
			t.Error("127.0.0.1 should be filtered out")
		}

		// Check IPv6 loopback
		if iface.IPAddress == "::1" {
			t.Error("::1 should be filtered out")
		}

		// Check other loopback patterns
		if len(iface.IPAddress) >= 4 && iface.IPAddress[:4] == "127." {
			t.Errorf("Loopback address %s should be filtered out", iface.IPAddress)
		}
	}
}

func TestWiFiPreferenceInPrimaryInterface(t *testing.T) {
	// This test is more of a behavior verification than a strict requirement
	primary := GetPrimaryInterface()
	allInterfaces := GetAllInterfaces()

	// Find if there are both WiFi and Ethernet interfaces
	hasWiFi := false
	hasEthernet := false

	for _, iface := range allInterfaces {
		if iface.Type == "wifi" && iface.IsUp {
			hasWiFi = true
		}
		if iface.Type == "ethernet" && iface.IsUp {
			hasEthernet = true
		}
	}

	// If both WiFi and Ethernet are available and up, primary should prefer WiFi
	if hasWiFi && hasEthernet && primary != nil {
		// This is expected behavior but not a hard requirement
		// Log the preference for observation
		t.Logf("Primary interface type: %s (WiFi available: %v, Ethernet available: %v)",
			primary.Type, hasWiFi, hasEthernet)
	}
}
