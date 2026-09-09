package app_interface

import (
	"net"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
)

type NetworkInterface struct {
	Name      string
	Index     int
	MTU       int
	Up        bool
	Multicast bool
	Loopback  bool
	P2P       bool
	Addresses []NetworkAddress

	Flags net.Flags
}

func (i *NetworkInterface) setFlagsValue() {
	if i.Up {
		i.Flags |= net.FlagUp
	}
	if i.Loopback {
		i.Flags |= net.FlagLoopback
	}
	if i.P2P {
		i.Flags |= net.FlagPointToPoint
	}
	if i.Multicast {
		i.Flags |= net.FlagMulticast
		i.Flags |= net.FlagBroadcast
	}
}

func (i *NetworkInterface) GetProtocolAddresses() []tcpip.ProtocolAddress {
	var addresses []tcpip.ProtocolAddress
	for _, a := range i.Addresses {
		parsed := net.ParseIP(a.Addr)
		if parsed == nil {
			continue
		}

		var raw net.IP
		var protocol tcpip.NetworkProtocolNumber
		if a.IsIPv6 {
			raw = parsed.To16()
			protocol = ipv6.ProtocolNumber
		} else {
			raw = parsed.To4()
			protocol = ipv4.ProtocolNumber
		}
		if raw == nil {
			continue
		}

		addresses = append(addresses, tcpip.ProtocolAddress{
			Protocol: protocol,
			AddressWithPrefix: tcpip.AddressWithPrefix{
				// This gVisor version stores network addresses as a string of raw
				// bytes. Casting the textual IP (for example "10.0.0.1") creates
				// an invalid 8-byte address; convert the parsed IP bytes instead.
				Address:   tcpip.Address(raw),
				PrefixLen: a.PrefixLength,
			},
		})
	}
	return addresses
}

func (i *NetworkInterface) Addrs() ([]NetworkAddress, error) {
	return i.Addresses, nil
}
