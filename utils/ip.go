package utils

import "net"

// IsLocalIP 检测 IP 地址是否为局域网 IP
func IsLocalIP(ip string) bool {
	address := net.ParseIP(ip)
	if address == nil {
		return false
	}
	if address.IsLoopback() {
		return true
	}
	if ip4 := address.To4(); ip4 != nil {
		// IPv4 局域网地址段
		return ip4[0] == 10 ||
			(ip4[0] == 172 && (ip4[1] >= 16 && ip4[1] < 32)) ||
			(ip4[0] == 192 && ip4[1] == 168)
	}
	return false
}
