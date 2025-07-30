package utils

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// InterfaceInfo represents information about a network interface
type InterfaceInfo struct {
	IPAddress  string `json:"ip_address"`
	MACAddress string `json:"mac_address"`
	Type       string `json:"type"` // "wifi", "ethernet", "other"
	IsUp       bool   `json:"is_up"`
}

func FormatDigits(n, width int) string {
	return PadLeft("0000", width, n)
}

func PadLeft(zeroes string, width int, n int) string {
	s := strconv.Itoa(n)

	// For negative numbers, don't pad with zeros before the minus sign
	if n < 0 {
		if len(s) >= width {
			return s[len(s)-width:]
		}
		return s
	}

	// For positive numbers, pad with zeros
	s = zeroes + s
	if len(s) < width {
		// Need more padding
		padding := make([]byte, width-len(s))
		for i := range padding {
			padding[i] = '0'
		}
		s = string(padding) + s
	}
	if len(s) < width {
		return s
	}
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

// detectInterfaceType attempts to determine if an interface is WiFi, Ethernet, or other
func detectInterfaceType(name string) string {
	name = strings.ToLower(name)

	// Common WiFi interface patterns
	wifiPrefixes := []string{"wlan", "wifi", "wlp", "wlo", "ath", "ra"}
	for _, prefix := range wifiPrefixes {
		if strings.HasPrefix(name, prefix) {
			return "wifi"
		}
	}

	// Common Ethernet interface patterns
	ethernetPrefixes := []string{"eth", "enp", "eno", "ens", "em", "p2p"}
	for _, prefix := range ethernetPrefixes {
		if strings.HasPrefix(name, prefix) {
			return "ethernet"
		}
	}

	// macOS interface patterns
	if strings.HasPrefix(name, "en") {
		// On macOS, en0 is usually WiFi and en1+ are usually Ethernet, but this varies
		// We'll classify as ethernet by default unless we have better detection
		return "ethernet"
	}

	// Windows interface patterns
	if strings.Contains(name, "wireless") || strings.Contains(name, "wi-fi") {
		return "wifi"
	}
	if strings.Contains(name, "ethernet") || strings.Contains(name, "local area connection") {
		return "ethernet"
	}

	return "other"
}

// GetAllInterfaces returns information about all network interfaces
func GetAllInterfaces() []InterfaceInfo {
	var result []InterfaceInfo

	interfaces, err := net.Interfaces()
	if err != nil {
		return result
	}

	for _, iface := range interfaces {
		// Get addresses for this interface
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		// Process each address on this interface
		for _, addr := range addrs {
			var ipAddr string
			if ipNet, ok := addr.(*net.IPNet); ok {
				ipAddr = ipNet.IP.String()
			} else if ip, ok := addr.(*net.IPAddr); ok {
				ipAddr = ip.IP.String()
			} else {
				continue
			}

			// Skip IPv6 link-local addresses
			if strings.HasPrefix(ipAddr, "fe80:") {
				continue
			}

			// Skip loopback interfaces (127.x.x.x and ::1)
			if strings.HasPrefix(ipAddr, "127.") || ipAddr == "::1" {
				continue
			}

			// Skip interfaces without IP addresses
			if ipAddr == "" || ipAddr == "0.0.0.0" {
				continue
			}

			macAddr := "00:00:00:00:00:00"
			if iface.HardwareAddr != nil {
				macAddr = iface.HardwareAddr.String()
			}

			interfaceInfo := InterfaceInfo{
				IPAddress:  ipAddr,
				MACAddress: macAddr,
				Type:       detectInterfaceType(iface.Name),
				IsUp:       iface.Flags&net.FlagUp != 0,
			}

			result = append(result, interfaceInfo)
		}
	}

	return result
}

// GetNetworkInterfaces returns only Ethernet and WiFi interfaces (excludes other types)
func GetNetworkInterfaces() []InterfaceInfo {
	allInterfaces := GetAllInterfaces()
	var networkInterfaces []InterfaceInfo

	for _, iface := range allInterfaces {
		// Only include Ethernet and WiFi interfaces
		if iface.Type == "ethernet" || iface.Type == "wifi" {
			networkInterfaces = append(networkInterfaces, iface)
		}
	}

	return networkInterfaces
}

// GetInterfaceByIP returns interface information for the interface with the specified IP address
func GetInterfaceByIP(ip string) *InterfaceInfo {
	interfaces := GetAllInterfaces()

	for _, iface := range interfaces {
		if iface.IPAddress == ip {
			return &iface
		}
	}

	return nil
}

// GetPrimaryInterface returns information about the primary network interface
// Priority: up, with IP address (WiFi preferred over Ethernet)
func GetPrimaryInterface() *InterfaceInfo {
	interfaces := GetAllInterfaces()

	var ethernetInterface *InterfaceInfo

	for _, iface := range interfaces {
		// Skip down interfaces
		if !iface.IsUp {
			continue
		}

		// Skip interfaces without valid IP addresses
		if iface.IPAddress == "" || iface.IPAddress == "0.0.0.0" {
			continue
		}

		// Prefer WiFi interfaces
		if iface.Type == "wifi" {
			return &iface
		}

		// Keep track of ethernet interface as fallback
		if iface.Type == "ethernet" && ethernetInterface == nil {
			ethernetInterface = &iface
		}
	}

	// Return ethernet interface if no WiFi found
	if ethernetInterface != nil {
		return ethernetInterface
	}

	// Return any valid interface as last resort
	for _, iface := range interfaces {
		if iface.IsUp && iface.IPAddress != "" && iface.IPAddress != "0.0.0.0" {
			return &iface
		}
	}

	return nil
}

// GetMacAddress returns the MAC address of the network interface that has the specified IP address
// If ip is empty, returns the MAC address of the primary network interface
// This function is kept for backward compatibility
func GetMacAddress(ip string) string {
	if ip == "" {
		if primary := GetPrimaryInterface(); primary != nil {
			return primary.MACAddress
		}
		return "00:00:00:00:00:00"
	}

	if iface := GetInterfaceByIP(ip); iface != nil {
		return iface.MACAddress
	}

	return "00:00:00:00:00:00"
}

func GetUptime() int64 {
	// Get the system uptime in seconds
	uptime, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0 // Return 0 if we can't read uptime
	}

	// Parse the uptime value
	var seconds float64
	if _, err := fmt.Sscanf(string(uptime), "%f", &seconds); err != nil {
		return 0 // Return 0 if parsing fails
	}

	return int64(seconds)
}
