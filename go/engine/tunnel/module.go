package tunnel

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net"
	"os"
	"sync"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster-android/go/app_interface"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
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
	tunQueueSize     = 1024
)

var (
	netStack *stack.Stack
	tunnelFD *os.File
	module   *modules.Module

	tunEndpoint     *channel.Endpoint
	tunBridgeCancel context.CancelFunc

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
							if stopErr := app_interface.SendServicesCommand("shutdown"); stopErr != nil {
								log.Warningf("vpn-service: failed to stop service after TUN setup failure: %s", stopErr)
							}
							continue
						}
						setLastError(nil)
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

func Enable() { eventChannel <- "connect" }
func Disable() { eventChannel <- "disconnect" }

func Reconnect() {
	select {
	case eventChannel <- "reconnect":
	default:
		log.Debug("vpn-service: reconnect already queued; coalescing network event")
	}
}

func SystemShutdown() { eventChannel <- "system-shutdown" }

func makeProtocolAddress(address string, prefix int) (tcpip.ProtocolAddress, error) {
	ip := net.ParseIP(address)
	if ip == nil {
		return tcpip.ProtocolAddress{}, fmt.Errorf("invalid tunnel IP %q", address)
	}
	if v4 := ip.To4(); v4 != nil {
		return tcpip.ProtocolAddress{
			Protocol: ipv4.ProtocolNumber,
			AddressWithPrefix: tcpip.AddressWithPrefix{
				Address:   tcpip.AddrFrom4Slice(v4),
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
			Address:   tcpip.AddrFrom16Slice(v6),
			PrefixLen: prefix,
		},
	}, nil
}

func networkProtocolForPacket(packet []byte) (tcpip.NetworkProtocolNumber, bool) {
	if len(packet) == 0 {
		return 0, false
	}
	switch packet[0] >> 4 {
	case 4:
		return ipv4.ProtocolNumber, true
	case 6:
		return ipv6.ProtocolNumber, true
	default:
		return 0, false
	}
}

// startTUNBridge connects Android's layer-3 TUN descriptor to gVisor's portable
// channel endpoint. This deliberately avoids gVisor's fdbased endpoint: modern
// fdbased is Linux-host specific and is not a supported Android GOOS surface.
func startTUNBridge(ctx context.Context, file *os.File, endpoint *channel.Endpoint) {
	go func() {
		buf := make([]byte, 65535)
		for {
			n, err := file.Read(buf)
			if err != nil {
				if ctx.Err() == nil && err != io.EOF {
					log.Warningf("vpn-service: TUN read stopped: %s", err)
				}
				return
			}
			if n == 0 {
				continue
			}
			proto, ok := networkProtocolForPacket(buf[:n])
			if !ok {
				log.Debug("vpn-service: dropping non-IP packet from layer-3 TUN")
				continue
			}
			data := append([]byte(nil), buf[:n]...)
			pkt := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(data)})
			endpoint.InjectInbound(proto, pkt)
			pkt.DecRef()
		}
	}()

	go func() {
		for {
			pkt := endpoint.ReadContext(ctx)
			if pkt == nil {
				return
			}
			packetBuffer := pkt.ToBuffer()
			data := packetBuffer.Flatten()
			written := 0
			for written < len(data) {
				n, err := file.Write(data[written:])
				if err != nil {
					packetBuffer.Release()
					pkt.DecRef()
					if ctx.Err() == nil {
						log.Warningf("vpn-service: TUN write stopped: %s", err)
					}
					return
				}
				if n == 0 {
					packetBuffer.Release()
					pkt.DecRef()
					log.Warning("vpn-service: TUN write returned zero bytes")
					return
				}
				written += n
			}
			packetBuffer.Release()
			pkt.DecRef()
		}
	}()
}

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

	file := os.NewFile(uintptr(fd), "tunnel")
	if file == nil {
		return fmt.Errorf("wrap tunnel file descriptor %d", fd)
	}

	var newStack *stack.Stack
	var newEndpoint *channel.Endpoint
	var bridgeCancel context.CancelFunc
	success := false
	defer func() {
		if success {
			return
		}
		if bridgeCancel != nil {
			bridgeCancel()
		}
		if newEndpoint != nil {
			newEndpoint.Close()
		}
		if newStack != nil {
			newStack.Close()
			newStack.Wait()
		}
		_ = file.Close()
	}()

	initializeRouter()
	log.Info("vpn-service: initializing tunnel interface")

	maddr, err := net.ParseMAC("aa:00:17:17:17:17")
	if err != nil {
		return fmt.Errorf("invalid tunnel MAC address: %w", err)
	}
	newEndpoint = channel.New(tunQueueSize, uint32(tunnelMTU), tcpip.LinkAddress(maddr))

	nicID := tcpip.NICID(1)
	newStack = stack.New(stack.Options{
		NetworkProtocols: []stack.NetworkProtocolFactory{ipv4.NewProtocol, ipv6.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol, icmp.NewProtocol4, icmp.NewProtocol6},
	})

	sackEnabledOpt := tcpip.TCPSACKEnabled(true)
	if tcpipErr := newStack.SetTransportProtocolOption(tcp.ProtocolNumber, &sackEnabledOpt); tcpipErr != nil {
		return fmt.Errorf("enable TCP SACK: %s", tcpipErr)
	}
	if tcpipErr := newStack.CreateNIC(nicID, newEndpoint); tcpipErr != nil {
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
	}{{tunnelIPv4, tunnelIPv4Prefix}, {tunnelIPv6, tunnelIPv6Prefix}} {
		protocolAddress, addrErr := makeProtocolAddress(spec.address, spec.prefix)
		if addrErr != nil {
			return addrErr
		}
		if tcpipErr := newStack.AddProtocolAddress(nicID, protocolAddress, stack.AddressProperties{
			PEB: stack.CanBePrimaryEndpoint,
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

	bridgeCtx, cancel := context.WithCancel(context.Background())
	bridgeCancel = cancel
	startTUNBridge(bridgeCtx, file, newEndpoint)

	tunnelFD = file
	tunEndpoint = newEndpoint
	tunBridgeCancel = bridgeCancel
	setActiveStack(newStack)
	success = true
	log.Info("vpn-service: tunnel interface ready")
	return nil
}

func destroyTunnelInterface() {
	log.Info("vpn-service: shutting down tunnel interface")
	EndAllConnections()

	if tunBridgeCancel != nil {
		tunBridgeCancel()
		tunBridgeCancel = nil
	}
	if tunEndpoint != nil {
		tunEndpoint.Close()
		tunEndpoint = nil
	}
	if s := takeActiveStack(); s != nil {
		s.Close()
		s.Wait()
	}
	if tunnelFD != nil {
		_ = tunnelFD.Close()
		tunnelFD = nil
	}
}

func IsActive() bool {
	stackLock.RLock()
	defer stackLock.RUnlock()
	return netStack != nil
}
