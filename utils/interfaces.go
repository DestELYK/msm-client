package utils

import (
	"net"
	"regexp"
	"strings"
)

// InterfaceInfo represents information about a network interface
type InterfaceInfo struct {
	IPAddress  string `json:"ip_address"`
	MACAddress string `json:"mac_address"`
	Type       string `json:"type"` // "wifi", "ethernet", "other"
	IsUp       bool   `json:"is_up"`
}

// IPv6 regex patterns
var (
	// Full IPv6 address (8 groups of 4 hex digits)
	ipv6FullRegex = regexp.MustCompile(`^([0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}$`)
	
	// Compressed IPv6 address (with :: notation)
	ipv6CompressedRegex = regexp.MustCompile(`^(([0-9a-fA-F]{1,4}:)*)?::([0-9a-fA-F]{1,4}:)*[0-9a-fA-F]{1,4}$|^::$|^::1$`)
	
	// IPv6 with embedded IPv4 (e.g., ::ffff:192.0.2.1)
	ipv6EmbeddedIPv4Regex = regexp.MustCompile(`^(([0-9a-fA-F]{1,4}:)*)?::(ffff:)?((25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)$`)
	
	// Link-local IPv6 addresses (fe80::/10)
	ipv6LinkLocalRegex = regexp.MustCompile(`^fe[89ab][0-9a-fA-F]:`)
)

// IsIPv6 determines if the given IP address is IPv6 using regex
func IsIPv6(ip string) bool {
	// Remove any zone identifier (e.g., %eth0)
	if idx := strings.LastIndex(ip, "%"); idx != -1 {
		ip = ip[:idx]
	}
	
	// Check against all IPv6 patterns
	return ipv6FullRegex.MatchString(ip) || 
		   ipv6CompressedRegex.MatchString(ip) || 
		   ipv6EmbeddedIPv4Regex.MatchString(ip)
}

// IsIPv6LinkLocal determines if the given IPv6 address is a link-local address
func IsIPv6LinkLocal(ip string) bool {
	// Remove any zone identifier (e.g., %eth0)
	if idx := strings.LastIndex(ip, "%"); idx != -1 {
		ip = ip[:idx]
	}
	
	return ipv6LinkLocalRegex.MatchString(ip)
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

			// Skip IPv6 link-local addresses using regex
			if IsIPv6LinkLocal(ipAddr) {
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

// GetIPv4Interfaces returns only interfaces with IPv4 addresses
func GetIPv4Interfaces() []InterfaceInfo {
	allInterfaces := GetAllInterfaces()
	var ipv4Interfaces []InterfaceInfo

	for _, iface := range allInterfaces {
		if !IsIPv6(iface.IPAddress) {
			ipv4Interfaces = append(ipv4Interfaces, iface)
		}
	}

	return ipv4Interfaces
}

// GetIPv6Interfaces returns only interfaces with IPv6 addresses
func GetIPv6Interfaces() []InterfaceInfo {
	allInterfaces := GetAllInterfaces()
	var ipv6Interfaces []InterfaceInfo

	for _, iface := range allInterfaces {
		if IsIPv6(iface.IPAddress) {
			ipv6Interfaces = append(ipv6Interfaces, iface)
		}
	}

	return ipv6Interfaces
}

// GetInterfaceIPVersion returns "ipv4", "ipv6", or "unknown" for the given IP address
func GetInterfaceIPVersion(ip string) string {
	if IsIPv6(ip) {
		return "ipv6"
	}
	
	// Simple IPv4 validation
	if net.ParseIP(ip) != nil && !IsIPv6(ip) {
		return "ipv4"
	}
	
	return "unknown"
}
