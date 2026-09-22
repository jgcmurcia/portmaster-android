package tunnel

import (
	"errors"
	"net"
	"syscall"
	"testing"

	"github.com/safing/portmaster-android/go/app_interface"
	"github.com/safing/portmaster/network/packet"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
)

func TestAddrStringUsesBracketedIPv6(t *testing.T) {
	addr := Addr{ip: net.ParseIP("2001:db8::53"), port: 53, ipVersion: packet.IPv6, protocol: packet.UDP}
	if got, want := addr.String(), "[2001:db8::53]:53"; got != want {
		t.Fatalf("Addr.String() = %q, want %q", got, want)
	}
}

func TestDirectIPv6DestinationReachesSocketProtection(t *testing.T) {
	previous := dialerNotTunneled
	t.Cleanup(func() { dialerNotTunneled = previous })
	denied := errors.New("test socket denied")
	var address string
	dialerNotTunneled = net.Dialer{Control: func(_, target string, _ syscall.RawConn) error {
		address = target
		return denied
	}}
	request := udp.NewForwarderRequest(nil, stack.TransportEndpointID{
		LocalAddress: tcpip.AddrFrom16([16]byte{0x26, 0x20, 0, 0xfe, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xfe}),
		LocalPort:    53,
	}, nil)
	if err := routeUDPThroughDefaultInterface(request); err == nil {
		t.Fatal("socket protection denial must fail")
	}
	if address != "[2620:fe::fe]:53" {
		t.Fatalf("socket protection received %q, want IPv6 DNS address", address)
	}
}

func TestUnprotectedSocketCannotDial(t *testing.T) {
	app_interface.RemoveServiceFunctionReference()
	initializeDialer()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	conn, err := dialerNotTunneled.Dial("tcp", listener.Addr().String())
	if conn != nil {
		conn.Close()
		t.Fatal("unprotected connection escaped the VPN")
	}
	if err == nil {
		t.Fatal("expected protect failure")
	}
}
