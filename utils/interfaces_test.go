package utils

import (
	"net"
	"strings"
	"testing"
)

func TestDetectInterfaceType(t *testing.T) {
	tests := []struct {
		name          string
		interfaceName string
		expected      string
	}{
		// WiFi interface patterns
		{
			name:          "wlan interface",
			interfaceName: "wlan0",
			expected:      "wifi",
		},
		{
			name:          "wifi interface",
			interfaceName: "wifi0",
			expected:      "wifi",
		},
		{
			name:          "wlp interface",
			interfaceName: "wlp2s0",
			expected:      "wifi",
		},
		{
			name:          "wlo interface",
			interfaceName: "wlo1",
			expected:      "wifi",
		},
		{
			name:          "ath interface",
			interfaceName: "ath0",
			expected:      "wifi",
		},
		{
			name:          "ra interface",
			interfaceName: "ra0",
			expected:      "wifi",
		},
		// Ethernet interface patterns
		{
			name:          "eth interface",
			interfaceName: "eth0",
			expected:      "ethernet",
		},
		{
			name:          "enp interface",
			interfaceName: "enp0s3",
			expected:      "ethernet",
		},
		{
			name:          "eno interface",
			interfaceName: "eno1",
			expected:      "ethernet",
		},
		{
			name:          "ens interface",
			interfaceName: "ens33",
			expected:      "ethernet",
		},
		{
			name:          "em interface",
			interfaceName: "em0",
			expected:      "ethernet",
		},
		{
			name:          "p2p interface",
			interfaceName: "p2p0",
			expected:      "ethernet",
		},
		// macOS patterns
		{
			name:          "en interface (macOS)",
			interfaceName: "en0",
			expected:      "ethernet",
		},
		{
			name:          "en interface with number",
			interfaceName: "en1",
			expected:      "ethernet",
		},
		// Windows patterns
		{
			name:          "wireless in name",
			interfaceName: "Local Area Connection* wireless",
			expected:      "wifi",
		},
		{
			name:          "wi-fi in name",
			interfaceName: "Wi-Fi",
			expected:      "wifi",
		},
		{
			name:          "ethernet in name",
			interfaceName: "Local Area Connection ethernet",
			expected:      "ethernet",
		},
		{
			name:          "local area connection",
			interfaceName: "Local Area Connection",
			expected:      "ethernet",
		},
		// Other/unknown patterns
		{
			name:          "lo interface (loopback)",
			interfaceName: "lo",
			expected:      "other",
		},
		{
			name:          "unknown interface",
			interfaceName: "unknown0",
			expected:      "other",
		},
		{
			name:          "custom interface",
			interfaceName: "custom123",
			expected:      "other",
		},
		// Case insensitive tests
		{
			name:          "uppercase wlan",
			interfaceName: "WLAN0",
			expected:      "wifi",
		},
		{
			name:          "mixed case eth",
			interfaceName: "ETH0",
			expected:      "ethernet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectInterfaceType(tt.interfaceName)
			if result != tt.expected {
				t.Errorf("detectInterfaceType(%q) = %q, expected %q", tt.interfaceName, result, tt.expected)
			}
		})
	}
}

func TestGetAllInterfaces(t *testing.T) {
	t.Run("Returns interfaces without error", func(t *testing.T) {
		interfaces := GetAllInterfaces()

		// Should not panic and return a slice (may be empty on some systems)
		if interfaces == nil {
			t.Error("GetAllInterfaces should not return nil")
		}

		// Verify structure of returned interfaces
		for i, iface := range interfaces {
			if iface.IPAddress == "" {
				t.Errorf("Interface %d has empty IP address", i)
			}
			if iface.MACAddress == "" {
				t.Errorf("Interface %d has empty MAC address", i)
			}
			if iface.Type == "" {
				t.Errorf("Interface %d has empty type", i)
			}

			// Type should be one of the valid types
			validTypes := []string{"wifi", "ethernet", "other"}
			found := false
			for _, validType := range validTypes {
				if iface.Type == validType {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Interface %d has invalid type: %q", i, iface.Type)
			}

			// IP address should be valid (basic validation)
			if !isValidIP(iface.IPAddress) {
				t.Errorf("Interface %d has invalid IP address: %q", i, iface.IPAddress)
			}

			// MAC address should have valid format (basic validation)
			if !isValidMAC(iface.MACAddress) {
				t.Errorf("Interface %d has invalid MAC address: %q", i, iface.MACAddress)
			}
		}
	})

	t.Run("Excludes loopback and invalid addresses", func(t *testing.T) {
		interfaces := GetAllInterfaces()

		for i, iface := range interfaces {
			// Should not include loopback addresses
			if strings.HasPrefix(iface.IPAddress, "127.") {
				t.Errorf("Interface %d includes loopback address: %q", i, iface.IPAddress)
			}
			if iface.IPAddress == "::1" {
				t.Errorf("Interface %d includes IPv6 loopback address", i)
			}

			// Should not include invalid addresses
			if iface.IPAddress == "0.0.0.0" {
				t.Errorf("Interface %d includes invalid address 0.0.0.0", i)
			}

			// Should not include IPv6 link-local
			if strings.HasPrefix(iface.IPAddress, "fe80:") {
				t.Errorf("Interface %d includes IPv6 link-local address: %q", i, iface.IPAddress)
			}
		}
	})
}

func TestGetNetworkInterfaces(t *testing.T) {
	t.Run("Returns only ethernet and wifi interfaces", func(t *testing.T) {
		interfaces := GetNetworkInterfaces()

		// Should not panic and return a slice
		if interfaces == nil {
			t.Error("GetNetworkInterfaces should not return nil")
		}

		// All returned interfaces should be ethernet or wifi
		for i, iface := range interfaces {
			if iface.Type != "ethernet" && iface.Type != "wifi" {
				t.Errorf("Interface %d has invalid type for network interfaces: %q", i, iface.Type)
			}
		}
	})

	t.Run("Subset of all interfaces", func(t *testing.T) {
		allInterfaces := GetAllInterfaces()
		networkInterfaces := GetNetworkInterfaces()

		// Network interfaces should be a subset of all interfaces
		if len(networkInterfaces) > len(allInterfaces) {
			t.Error("Network interfaces count should not exceed all interfaces count")
		}

		// Every network interface should exist in all interfaces
		for _, netIface := range networkInterfaces {
			found := false
			for _, allIface := range allInterfaces {
				if netIface.IPAddress == allIface.IPAddress &&
					netIface.MACAddress == allIface.MACAddress {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Network interface %v not found in all interfaces", netIface)
			}
		}
	})
}

func TestGetInterfaceByIP(t *testing.T) {
	// Get actual interfaces for testing
	interfaces := GetAllInterfaces()

	if len(interfaces) == 0 {
		t.Skip("No interfaces available for testing")
	}

	t.Run("Find existing interface", func(t *testing.T) {
		// Use first interface for testing
		targetIP := interfaces[0].IPAddress
		found := GetInterfaceByIP(targetIP)

		if found == nil {
			t.Errorf("GetInterfaceByIP should find interface with IP %q", targetIP)
		} else {
			if found.IPAddress != targetIP {
				t.Errorf("Found interface has wrong IP: expected %q, got %q", targetIP, found.IPAddress)
			}
		}
	})

	t.Run("Non-existent IP", func(t *testing.T) {
		found := GetInterfaceByIP("192.168.999.999")
		if found != nil {
			t.Error("GetInterfaceByIP should return nil for non-existent IP")
		}
	})

	t.Run("Empty IP", func(t *testing.T) {
		found := GetInterfaceByIP("")
		if found != nil {
			t.Error("GetInterfaceByIP should return nil for empty IP")
		}
	})

	t.Run("Invalid IP format", func(t *testing.T) {
		found := GetInterfaceByIP("not-an-ip")
		if found != nil {
			t.Error("GetInterfaceByIP should return nil for invalid IP format")
		}
	})
}

func TestGetPrimaryInterface(t *testing.T) {
	t.Run("Returns valid interface or nil", func(t *testing.T) {
		primary := GetPrimaryInterface()

		// May be nil on systems without valid interfaces
		if primary != nil {
			// Verify it's a valid interface
			if primary.IPAddress == "" {
				t.Error("Primary interface should have valid IP address")
			}
			if primary.MACAddress == "" {
				t.Error("Primary interface should have valid MAC address")
			}
			if !primary.IsUp {
				t.Error("Primary interface should be up")
			}
			if primary.IPAddress == "0.0.0.0" {
				t.Error("Primary interface should not have 0.0.0.0 address")
			}
		}
	})

	t.Run("Prefers wifi over ethernet", func(t *testing.T) {
		// This test is environment-dependent, so we'll just verify
		// that if we have both wifi and ethernet, wifi is preferred
		allInterfaces := GetAllInterfaces()

		hasWifi := false
		hasEthernet := false

		for _, iface := range allInterfaces {
			if iface.IsUp && iface.IPAddress != "" && iface.IPAddress != "0.0.0.0" {
				if iface.Type == "wifi" {
					hasWifi = true
				} else if iface.Type == "ethernet" {
					hasEthernet = true
				}
			}
		}

		primary := GetPrimaryInterface()

		// If we have both wifi and ethernet, primary should be wifi
		if hasWifi && hasEthernet && primary != nil {
			if primary.Type != "wifi" {
				t.Error("Primary interface should prefer wifi over ethernet when both are available")
			}
		}
	})
}

func TestGetMacAddress(t *testing.T) {
	t.Run("Empty IP returns primary interface MAC", func(t *testing.T) {
		mac := GetMacAddress("")

		// Should return a valid MAC address format or default
		if !isValidMAC(mac) {
			t.Errorf("GetMacAddress(\"\") returned invalid MAC: %q", mac)
		}

		// Compare with primary interface
		primary := GetPrimaryInterface()
		if primary != nil {
			if mac != primary.MACAddress {
				t.Errorf("GetMacAddress(\"\") should match primary interface MAC")
			}
		} else {
			// No primary interface, should return default
			if mac != "00:00:00:00:00:00" {
				t.Errorf("GetMacAddress(\"\") should return default MAC when no primary interface")
			}
		}
	})

	t.Run("Valid IP returns correct MAC", func(t *testing.T) {
		interfaces := GetAllInterfaces()
		if len(interfaces) == 0 {
			t.Skip("No interfaces available for testing")
		}

		// Test with first interface
		targetIP := interfaces[0].IPAddress
		expectedMAC := interfaces[0].MACAddress

		mac := GetMacAddress(targetIP)
		if mac != expectedMAC {
			t.Errorf("GetMacAddress(%q) = %q, expected %q", targetIP, mac, expectedMAC)
		}
	})

	t.Run("Invalid IP returns default MAC", func(t *testing.T) {
		mac := GetMacAddress("192.168.999.999")
		if mac != "00:00:00:00:00:00" {
			t.Errorf("GetMacAddress with invalid IP should return default MAC, got %q", mac)
		}
	})

	t.Run("Malformed IP returns default MAC", func(t *testing.T) {
		mac := GetMacAddress("not-an-ip")
		if mac != "00:00:00:00:00:00" {
			t.Errorf("GetMacAddress with malformed IP should return default MAC, got %q", mac)
		}
	})
}

// Helper functions for validation
func isValidIP(ip string) bool {
	parsed := net.ParseIP(ip)
	return parsed != nil
}

func isValidMAC(mac string) bool {
	if mac == "00:00:00:00:00:00" {
		return true // Default MAC is valid
	}
	_, err := net.ParseMAC(mac)
	return err == nil
}

// Test the InterfaceInfo struct
func TestInterfaceInfo(t *testing.T) {
	t.Run("Create and validate InterfaceInfo", func(t *testing.T) {
		info := InterfaceInfo{
			IPAddress:  "192.168.1.100",
			MACAddress: "aa:bb:cc:dd:ee:ff",
			Type:       "ethernet",
			IsUp:       true,
		}

		if info.IPAddress != "192.168.1.100" {
			t.Error("IPAddress not set correctly")
		}
		if info.MACAddress != "aa:bb:cc:dd:ee:ff" {
			t.Error("MACAddress not set correctly")
		}
		if info.Type != "ethernet" {
			t.Error("Type not set correctly")
		}
		if !info.IsUp {
			t.Error("IsUp not set correctly")
		}
	})
}

// Benchmark tests
func BenchmarkGetAllInterfaces(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GetAllInterfaces()
	}
}

func BenchmarkGetNetworkInterfaces(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GetNetworkInterfaces()
	}
}

func BenchmarkGetPrimaryInterface(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GetPrimaryInterface()
	}
}

func BenchmarkDetectInterfaceType(b *testing.B) {
	interfaceNames := []string{"eth0", "wlan0", "enp0s3", "wlp2s0", "lo", "unknown"}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for _, name := range interfaceNames {
			detectInterfaceType(name)
		}
	}
}

func BenchmarkGetInterfaceByIP(b *testing.B) {
	// Get a valid IP for benchmarking
	interfaces := GetAllInterfaces()
	if len(interfaces) == 0 {
		b.Skip("No interfaces available for benchmarking")
	}
	testIP := interfaces[0].IPAddress

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GetInterfaceByIP(testIP)
	}
}

func BenchmarkGetMacAddress(b *testing.B) {
	// Get a valid IP for benchmarking
	interfaces := GetAllInterfaces()
	if len(interfaces) == 0 {
		b.Skip("No interfaces available for benchmarking")
	}
	testIP := interfaces[0].IPAddress

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GetMacAddress(testIP)
	}
}
