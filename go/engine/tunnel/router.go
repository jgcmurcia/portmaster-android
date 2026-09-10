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
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
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
				// Never open an outbound socket unless Android confirmed that it is
				// excluded from this VpnService. Otherwise the socket can recurse
				// into our own TUN or escape routing assumptions.
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

	remoteAddr := Addr{
		ip:        net.IP(fr.ID().LocalAddress),
		port:      fr.ID().LocalPort,
		ipVersion: ipVersion,
		protocol:  packet.TCP,
	}

	localAddr := Addr{
		ip:        net.IP(fr.ID().RemoteAddress),
		port:      fr.ID().RemotePort,
		ipVersion: ipVersion,
		protocol:  packet.TCP,
	}

	var wq waiter.Queue
	ep, err := fr.CreateEndpoint(&wq)
	if err != nil {
		return fmt.Errorf("failed to create endpoint for: %s", err)
	}

	systemConn := gonet.NewTCPConn(&wq, ep)
	addSPNConnection(systemConn, localAddr, remoteAddr)

	return nil
}

func routeUDPThroughSPN(stack *stack.Stack, fr *udp.ForwarderRequest) error {
	ipVersion := packet.IPv4
	if strings.Contains(fr.ID().LocalAddress.String(), ":") {
		ipVersion = packet.IPv6
	}

	remoteAddr := Addr{
		ip:        net.IP(fr.ID().LocalAddress),
		port:      fr.ID().LocalPort,
		ipVersion: ipVersion,
		protocol:  packet.UDP,
	}

	localAddr := Addr{
		ip:        net.IP(fr.ID().RemoteAddress),
		port:      fr.ID().RemotePort,
		ipVersion: ipVersion,
		protocol:  packet.UDP,
	}

	var wq waiter.Queue
	ep, err := fr.CreateEndpoint(&wq)
	if err != nil {
		return fmt.Errorf("failed to create endpoint for: %s", err)
	}

	systemConn := gonet.NewUDPConn(stack, &wq, ep)
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
		remoteConn.Close()
		return fmt.Errorf("failed to create endpoint for remote %s: %s", remote, err)
	}

	systemConn := gonet.NewTCPConn(&wq, ep)
	addDefaultConnection(systemConn, remoteConn, ep)
	return nil
}

func routeUDPThroughDefaultInterface(stack *stack.Stack, fr *udp.ForwarderRequest) error {
	remote := fmt.Sprintf("%s:%d", fr.ID().LocalAddress.String(), fr.ID().LocalPort)
	remoteConn, udpErr := dialerNotTunneled.Dial("udp", remote)
	if udpErr != nil {
		return fmt.Errorf("failed to establish connection to remote host %s: %s", remote, udpErr)
	}

	var wq waiter.Queue
	ep, err := fr.CreateEndpoint(&wq)
	if err != nil {
		remoteConn.Close()
		return fmt.Errorf("failed to create endpoint for remote %s: %s", remote, err)
	}

	systemConn := gonet.NewUDPConn(stack, &wq, ep)
	addDefaultConnection(systemConn, remoteConn, ep)
	return nil
}

func DefaultTCPRouting(fr *tcp.ForwarderRequest) error {
	ipAddress := net.IP(fr.ID().LocalAddress)
	scope := netutils.GetIPScope(ipAddress)

	// SPN's own control/bootstrap connections must leave on the protected
	// physical socket or the overlay could recursively route through itself.
	if captain.IsExcepted(ipAddress) {
		return routeTCPThroughDefaultInterface(fr)
	}

	if scope == netutils.Global && isSpnEnabled() {
		if !captain.ClientReady() {
			// Privacy invariant: once the user enabled SPN, never silently fall
			// back to the real Internet path while SPN is reconnecting or failed.
			return fmt.Errorf("SPN is enabled but not ready; blocking TCP connection to %s", ipAddress)
		}
		return routeTCPThroughSPN(fr)
	}

	return routeTCPThroughDefaultInterface(fr)
}

func getUidOfTCPRequest(fr *tcp.ForwarderRequest) (int, error) {
	conn := app_interface.Connection{
		Protocol: 6, // TCP

		LocalIP:   net.IP(fr.ID().RemoteAddress),
		LocalPort: int(fr.ID().RemotePort),

		RemoteIP:   net.IP(fr.ID().LocalAddress),
		RemotePort: int(fr.ID().LocalPort),
	}
	return app_interface.GetConnectionOwner(conn)
}

func DefaultUDPRouting(stack *stack.Stack, fr *udp.ForwarderRequest) error {
	ipAddress := net.IP(fr.ID().LocalAddress)
	scope := netutils.GetIPScope(ipAddress)

	// Apply the same recursion exception to UDP. SPN transports may use UDP and
	// must be able to reach their bootstrap/home-hub endpoints outside the TUN.
	if captain.IsExcepted(ipAddress) {
		return routeUDPThroughDefaultInterface(stack, fr)
	}

	if scope == netutils.Global && isSpnEnabled() {
		if !captain.ClientReady() {
			return fmt.Errorf("SPN is enabled but not ready; blocking UDP connection to %s", ipAddress)
		}
		return routeUDPThroughSPN(stack, fr)
	}

	return routeUDPThroughDefaultInterface(stack, fr)
}

// func handleDNS(stack *stack.Stack, fr *udp.ForwarderRequest) {
// 	var wq waiter.Queue
// 	ep, err := fr.CreateEndpoint(&wq)
// 	if err != nil {
// 		log.Errorf("failed to handle udp request: %s", err.String())
// 		return
// 	}

// 	c := gonet.NewUDPConn(stack, &wq, ep)

// 	go func() {
// 		defer c.Close()
// 		c.SetReadDeadline(time.Now().Add(10 * time.Second))

// 		var frame [1500]byte
// 		n, err := c.Read(frame[:])
// 		if err != nil {
// 			return
// 		}
// 		response, err := ResolveQuery(frame[:n])
// 		if err == nil {
// 			c.Write(response)
// 		}
// 		// packet := gopacket.NewPacket(frame[:n], layers.LayerTypeDNS, gopacket.Default)

// 		// dns, _ := packet.Layer(layers.DNS)

// 		// dns, _ := packet.Layer(layers.LayerTypeDNS).(*layers.DNS)

// 		// log.Infof("tunnel: Question: %s", string(dns.Questions[0].Name))
// 		// remoteConn.Write(frame[:n])
// 		// n, err = remoteConn.Read(frame[:])

// 	}()
// }

func EndAllConnections() {
	endAllDefaultConnections()
}
