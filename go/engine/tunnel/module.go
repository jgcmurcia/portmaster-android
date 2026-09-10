package tunnel

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"os"
	"sync"
	"syscall"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster-android/go/app_interface"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/fdbased"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/icmp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
)

const (
	tunnelIPv4       = "100.127.247.245"
	tunnelIPv4Prefix = 30
	tunnelIPv6       = "fd00:1::1"
	tunnelIPv6Prefix = 64
	tunnelMTU        = 1400
)

var (
	netStack *stack.Stack
	tunnelFD *os.File
	module   *modules.Module

	eventChannel chan string

	stackLock       sync.RWMutex
	stateLock       sync.RWMutex
	tunnelLastError string
)

func init() {
	eventChannel = make(chan string, 32)
	module = modules.Register("vpn-service", nil, start, nil, "base")
	module.Enable()
}

func setLastError(err error) {
	stateLock.Lock()
	defer stateLock.Unlock()
	if err == nil {
		tunnelLastError = ""
		return
	}
	tunnelLastError = err.Error()
}

// LastError returns the most recent TUN setup/reconnect error for the Android UI.
func LastError() string {
	stateLock.RLock()
	defer stateLock.RUnlock()
	return tunnelLastError
}

func setActiveStack(s *stack.Stack) {
	stackLock.Lock()
	netStack = s
	stackLock.Unlock()
}

func takeActiveStack() *stack.Stack {
	stackLock.Lock()
	s := netStack
	netStack = nil
	stackLock.Unlock()
	return s
}

func start() error {
	module.StartServiceWorker("vpn-service-manager", 0, func(ctx context.Context) error {
		for {
			select {
			case command := <-eventChannel:
				switch command {
				case "connect":
					if !IsActive() {
						if err := setupTunnelInterface(); err != nil {
							setLastError(err)
							log.Errorf("vpn-service: failed to set up tunnel: %s", err)
							// The TUN file descriptor has already been closed by the
							// failed setup path. Do not leave an Android foreground
							// service running and visually suggesting protection.
							if stopErr := app_interface.SendServicesCommand("shutdown"); stopErr != nil {
								log.Warningf("vpn-service: failed to stop service after TUN setup failure: %s", stopErr)
							}
							continue
						}
						setLastError(nil)

						// Keep Android's foreground VPN service alive only after
						// the userspace network stack is ready.
						if err := app_interface.SendServicesCommand("keep_alive"); err != nil {
							log.Warningf("vpn-service: failed to send keep-alive: %s", err)
						}
					}

				case "disconnect":
					destroyTunnelInterface()
					setLastError(nil)
					if err := app_interface.SendServicesCommand("shutdown"); err != nil {
						log.Warningf("vpn-service: failed to stop Android service: %s", err)
					}

				case "reconnect":
					destroyTunnelInterface()
					if err := setupTunnelInterface(); err != nil {
						setLastError(err)
						log.Errorf("vpn-service: failed to reconnect tunnel: %s", err)
						if stopErr := app_interface.SendServicesCommand("shutdown"); stopErr != nil {
							log.Warningf("vpn-service: failed to stop service after reconnect failure: %s", stopErr)
						}
						continue
					}
					setLastError(nil)

				case "system-shutdown":
					log.Errorf("vpn-service: the VPN service has stopped, restart the app to start it again.")
					notification := &app_interface.Notification{ID: rand.Int31()}
					notification.Title = "The system stopped Portmaster"
					notification.Message = "Tap here to restart it"
					_ = app_interface.ShowNotification(notification)
					destroyTunnelInterface()
					_ = app_interface.SendServicesCommand("shutdown")
				}

			case <-ctx.Done():
				destroyTunnelInterface()
				_ = app_interface.SendServicesCommand("shutdown")
				return nil
			}
		}
	})
	return nil
}

func Enable() {
	eventChannel <- "connect"
}

func Disable() {
	eventChannel <- "disconnect"
}

func Reconnect() {
	// Network callbacks can arrive in bursts. A reconnect is idempotent and
	// already rebuilds the complete stack, so coalesce bursts rather than ever
	// blocking Android's callback/main thread on a full Go channel.
	select {
	case eventChannel <- "reconnect":
	default:
		log.Debug("vpn-service: reconnect already queued; coalescing network event")
	}
}

func SystemShutdown() {
	eventChannel <- "system-shutdown"
}

func makeProtocolAddress(address string, prefix int) (tcpip.ProtocolAddress, error) {
	ip := net.ParseIP(address)
	if ip == nil {
		return tcpip.ProtocolAddress{}, fmt.Errorf("invalid tunnel IP %q", address)
	}

	if v4 := ip.To4(); v4 != nil {
		return tcpip.ProtocolAddress{
			Protocol: ipv4.ProtocolNumber,
			AddressWithPrefix: tcpip.AddressWithPrefix{
				Address:   tcpip.Address(v4),
				PrefixLen: prefix,
			},
		}, nil
	}

	v6 := ip.To16()
	if v6 == nil {
		return tcpip.ProtocolAddress{}, fmt.Errorf("invalid IPv6 tunnel IP %q", address)
	}
	return tcpip.ProtocolAddress{
		Protocol: ipv6.ProtocolNumber,
		AddressWithPrefix: tcpip.AddressWithPrefix{
			Address:   tcpip.Address(v6),
			PrefixLen: prefix,
		},
	}, nil
}

// setupTunnelInterface establishes the Android TUN and attaches gVisor.
//
// Do not rediscover the TUN through java.net.NetworkInterface here. Android can
// publish a newly established VpnService interface asynchronously. The old code
// could therefore leave the OS VPN active while gVisor had no NIC addresses.
// We created the TUN ourselves, so configure the exact same addresses directly.
func setupTunnelInterface() (err error) {
	if IsActive() {
		return nil
	}

	fd, err := app_interface.VPNInit()
	if err != nil {
		return fmt.Errorf("initialize VPN file descriptor: %w", err)
	}
	if fd <= 0 {
		return fmt.Errorf("invalid tunnel file descriptor: %d", fd)
	}

	tunnelFD = os.NewFile(uintptr(fd), "tunnel")
	if tunnelFD == nil {
		return fmt.Errorf("wrap tunnel file descriptor %d", fd)
	}

	var newStack *stack.Stack
	// Ensure any partial setup is torn down on every error path. The stack is
	// published to IsActive() only after all addresses/forwarders are installed.
	success := false
	defer func() {
		if success {
			return
		}
		if newStack != nil {
			newStack.Close()
			newStack.Wait()
		}
		if tunnelFD != nil {
			_ = tunnelFD.Close()
			tunnelFD = nil
		}
	}()

	initializeRouter()
	log.Info("vpn-service: initializing tunnel interface")

	maddr, err := net.ParseMAC("aa:00:17:17:17:17")
	if err != nil {
		return fmt.Errorf("invalid tunnel MAC address: %w", err)
	}

	if err := syscall.SetNonblock(fd, true); err != nil {
		return fmt.Errorf("set tunnel non-blocking: %w", err)
	}

	linkID, err := fdbased.New(&fdbased.Options{
		FDs:            []int{fd},
		MTU:            uint32(tunnelMTU),
		EthernetHeader: false,
		Address:        tcpip.LinkAddress(maddr),
		ClosedFunc: func(err tcpip.Error) {
			if err != nil {
				log.Errorf("vpn-service: file descriptor closed: %s", err)
			}
		},
	})
	if err != nil {
		return fmt.Errorf("create gVisor link endpoint: %s", err)
	}

	nicID := tcpip.NICID(1)
	newStack = stack.New(stack.Options{
		NetworkProtocols: []stack.NetworkProtocolFactory{
			ipv4.NewProtocol,
			ipv6.NewProtocol,
		},
		TransportProtocols: []stack.TransportProtocolFactory{
			tcp.NewProtocol,
			udp.NewProtocol,
			icmp.NewProtocol4,
			icmp.NewProtocol6,
		},
	})

	sackEnabledOpt := tcpip.TCPSACKEnabled(true)
	if tcpipErr := newStack.SetTransportProtocolOption(tcp.ProtocolNumber, &sackEnabledOpt); tcpipErr != nil {
		return fmt.Errorf("enable TCP SACK: %s", tcpipErr)
	}

	if tcpipErr := newStack.CreateNIC(nicID, linkID); tcpipErr != nil {
		return fmt.Errorf("create gVisor NIC: %s", tcpipErr)
	}

	newStack.SetSpoofing(nicID, true)
	newStack.SetRouteTable([]tcpip.Route{
		{Destination: header.IPv4EmptySubnet, NIC: nicID},
		{Destination: header.IPv6EmptySubnet, NIC: nicID},
	})

	for _, spec := range []struct {
		address string
		prefix  int
	}{
		{tunnelIPv4, tunnelIPv4Prefix},
		{tunnelIPv6, tunnelIPv6Prefix},
	} {
		protocolAddress, addrErr := makeProtocolAddress(spec.address, spec.prefix)
		if addrErr != nil {
			return addrErr
		}
		if tcpipErr := newStack.AddProtocolAddress(nicID, protocolAddress, stack.AddressProperties{
			PEB:        stack.CanBePrimaryEndpoint,
			ConfigType: stack.AddressConfigStatic,
		}); tcpipErr != nil {
			return fmt.Errorf("add tunnel address %s: %s", spec.address, tcpipErr)
		}
	}

	if tcpipErr := newStack.SetPromiscuousMode(nicID, true); tcpipErr != nil {
		return fmt.Errorf("enable promiscuous mode: %s", tcpipErr)
	}

	tcpForwarder := tcp.NewForwarder(newStack, 0, 32, func(fr *tcp.ForwarderRequest) {
		if routeErr := DefaultTCPRouting(fr); routeErr != nil {
			log.Debugf("vpn-service: TCP routing failed: %s", routeErr)
			fr.Complete(true)
			return
		}
		fr.Complete(false)
	})
	newStack.SetTransportProtocolHandler(tcp.ProtocolNumber, tcpForwarder.HandlePacket)

	udpForwarder := udp.NewForwarder(newStack, func(fr *udp.ForwarderRequest) {
		if routeErr := DefaultUDPRouting(newStack, fr); routeErr != nil {
			log.Debugf("vpn-service: UDP routing failed: %s", routeErr)
		}
	})
	newStack.SetTransportProtocolHandler(udp.ProtocolNumber, udpForwarder.HandlePacket)

	setActiveStack(newStack)
	success = true
	log.Info("vpn-service: tunnel interface ready")
	return nil
}

func destroyTunnelInterface() {
	log.Info("vpn-service: shutting down tunnel interface")

	EndAllConnections()

	if s := takeActiveStack(); s != nil {
		s.Close()
		s.Wait()
	}

	if tunnelFD != nil {
		_ = tunnelFD.Close()
		tunnelFD = nil
	}
}

// IsActive checks if the userspace tunnel stack is fully initialized.
func IsActive() bool {
	stackLock.RLock()
	defer stackLock.RUnlock()
	return netStack != nil
}
