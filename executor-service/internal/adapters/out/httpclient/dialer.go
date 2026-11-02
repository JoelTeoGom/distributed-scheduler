package httpclient

import (
	"fmt"
	"net"
	"net/netip"
	"syscall"
)

// DNS can change after the task-service validated the URL, so the resolved address is checked on every connection.
func rejectInternalAddress(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}

	ip, err := netip.ParseAddr(host)
	if err != nil {
		return err
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return fmt.Errorf("address %s is internal", ip)
	}
	return nil
}
