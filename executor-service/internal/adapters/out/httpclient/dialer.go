package httpclient

import (
	"net/netip"
	"syscall"

	"github.com/JoelTeoGom/distributed-scheduler/executor-service/internal/domain"
)

var allowedPorts = map[uint16]bool{
	80:  true,
	443: true,
}

// Ranges that IsGlobalUnicast and IsPrivate let through.
var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), // CGNAT, used as internal space by some clouds
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), // NAT64 and 6to4 embed an IPv4
	netip.MustParsePrefix("2002::/16"),
}

// DNS can change after the task-service validated the URL, so the resolved address is checked on every connection.
func checkDestination(network, address string, _ syscall.RawConn) error {
	if network != "tcp4" && network != "tcp6" {
		return domain.ErrDestinationBlocked
	}

	addressPort, err := netip.ParseAddrPort(address)
	if err != nil || !allowedPorts[addressPort.Port()] || !isPublic(addressPort.Addr()) {
		return domain.ErrDestinationBlocked
	}
	return nil
}

func isPublic(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return false
	}
	for _, prefix := range blockedPrefixes {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}
