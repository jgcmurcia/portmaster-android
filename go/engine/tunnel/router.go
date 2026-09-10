package tunnel

import (
	"fmt"
	"net"
	"strings"
	"syscall"

	"github.com/safing/portbase/config"
	"github.com/safing/portbase/database"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster-android/go/app_interface"
	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/spn/captain"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

var (
	isSpnEnabled      config.BoolOption
	dialerNotTunneled net.Dialer
	dbInterface       *database.Interface
	ownUID            int
)

func gvisorIP(addr tcpip.Address) net.IP {
	return net.IP(append([]byte(nil), addr.AsSlice()...))
}

func initializeRouter() {
	initializeDialer()
	isSpnEnabled = config.Concurrent.GetAsBool(captain.CfgOptionEnableSPNKey, false)
	var err error
	ownUID, err = app_interface.GetAppUID()
	if err != nil {
		log.Errorf("tunnel: failed to get app UID: %s", err)
	}
}

func initializeDialer() {
	dialerNotTunneled = net.Dialer{
		Control: func(network, address string, c syscall.RawConn) error {
			var protectErr error
			if err := c.Control(func(fd uintptr) {
				protectErr = app_interface.SetDefaultInterfaceForSocket(fd)
			}); err != nil {
				return fmt.Errorf("access socket control for %s: %w", address, err)
			}
			if protectErr != nil {
				return fmt.Errorf("protect outbound socket for %s: %w", address, protectErr)
			}
			return nil
		},
	}
}

func routeTCPThroughSPN(fr *tcp.ForwarderRequest) error {
	ipVersion := packet.IPv4
	if strings.Contains(fr.ID().LocalAddress.String(), ":") {
		ipVersion = packet.IPv6
	}
	remoteAddr := Addr{ip: gvisorIP(fr.ID().LocalAddress), port: fr.ID().LocalPort, ipVersion: ipVersion, protocol: packet.TCP}
	localAddr := Addr{ip: gvisorIP(fr.ID().RemoteAddress), port: fr.ID().RemotePort, ipVersion: ipVersion, protocol: packet.TCP}

	var wq waiter.Queue
	ep, err := fr.CreateEndpoint(&wq)
	if err != nil {
		return fmt.Errorf("failed to create endpoint for: %s", err)
	}
	systemConn := gonet.NewTCPConn(&wq, ep)
	addSPNConnection(systemConn, localAddr, remoteAddr)
	return nil
}

func routeUDPThroughSPN(fr *udp.ForwarderRequest) error {
	ipVersion := packet.IPv4
	if strings.Contains(fr.ID().LocalAddress.String(), ":") {
		ipVersion = packet.IPv6
	}
	remoteAddr := Addr{ip: gvisorIP(fr.ID().LocalAddress), port: fr.ID().LocalPort, ipVersion: ipVersion, protocol: packet.UDP}
	localAddr := Addr{ip: gvisorIP(fr.ID().RemoteAddress), port: fr.ID().RemotePort, ipVersion: ipVersion, protocol: packet.UDP}

	var wq waiter.Queue
	ep, err := fr.CreateEndpoint(&wq)
	if err != nil {
		return fmt.Errorf("failed to create endpoint for: %s", err)
	}
	systemConn := gonet.NewUDPConn(&wq, ep)
	addSPNConnection(systemConn, localAddr, remoteAddr)
	return nil
}

func routeTCPThroughDefaultInterface(fr *tcp.ForwarderRequest) error {
	remote := fmt.Sprintf("%s:%d", fr.ID().LocalAddress.String(), fr.ID().LocalPort)
	remoteConn, tcpErr := dialerNotTunneled.Dial("tcp", remote)
	if tcpErr != nil {
		return fmt.Errorf("failed to establish connection to remote host %s: %s", remote, tcpErr)
	}

	var wq waiter.Queue
	ep, err := fr.CreateEndpoint(&wq)
	if err != nil {
		_ = remoteConn.Close()
		return fmt.Errorf("failed to create endpoint for remote %s: %s", remote, err)
	}
	systemConn := gonet.NewTCPConn(&wq, ep)
	addDefaultConnection(systemConn, remoteConn, ep)
	return nil
}

func routeUDPThroughDefaultInterface(fr *udp.ForwarderRequest) error {
	remote := fmt.Sprintf("%s:%d", fr.ID().LocalAddress.String(), fr.ID().LocalPort)
	remoteConn, udpErr := dialerNotTunneled.Dial("udp", remote)
	if udpErr != nil {
		return fmt.Errorf("failed to establish connection to remote host %s: %s", remote, udpErr)
	}

	var wq waiter.Queue
	ep, err := fr.CreateEndpoint(&wq)
	if err != nil {
		_ = remoteConn.Close()
		return fmt.Errorf("failed to create endpoint for remote %s: %s", remote, err)
	}
	systemConn := gonet.NewUDPConn(&wq, ep)
	addDefaultConnection(systemConn, remoteConn, ep)
	return nil
}

func DefaultTCPRouting(fr *tcp.ForwarderRequest) error {
	ipAddress := gvisorIP(fr.ID().LocalAddress)
	scope := netutils.GetIPScope(ipAddress)
	if captain.IsExcepted(ipAddress) {
		return routeTCPThroughDefaultInterface(fr)
	}
	if scope == netutils.Global && isSpnEnabled() {
		if !captain.ClientReady() {
			return fmt.Errorf("SPN is enabled but not ready; blocking TCP connection to %s", ipAddress)
		}
		return routeTCPThroughSPN(fr)
	}
	return routeTCPThroughDefaultInterface(fr)
}

func getUidOfTCPRequest(fr *tcp.ForwarderRequest) (int, error) {
	conn := app_interface.Connection{
		Protocol:   6,
		LocalIP:    gvisorIP(fr.ID().RemoteAddress),
		LocalPort:  int(fr.ID().RemotePort),
		RemoteIP:   gvisorIP(fr.ID().LocalAddress),
		RemotePort: int(fr.ID().LocalPort),
	}
	return app_interface.GetConnectionOwner(conn)
}

func DefaultUDPRouting(fr *udp.ForwarderRequest) error {
	ipAddress := gvisorIP(fr.ID().LocalAddress)
	scope := netutils.GetIPScope(ipAddress)
	if captain.IsExcepted(ipAddress) {
		return routeUDPThroughDefaultInterface(fr)
	}
	if scope == netutils.Global && isSpnEnabled() {
		if !captain.ClientReady() {
			return fmt.Errorf("SPN is enabled but not ready; blocking UDP connection to %s", ipAddress)
		}
		return routeUDPThroughSPN(fr)
	}
	return routeUDPThroughDefaultInterface(fr)
}

func EndAllConnections() {
	endAllDefaultConnections()
}
