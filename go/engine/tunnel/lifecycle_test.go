package tunnel

import (
	"errors"
	"syscall"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/safing/portmaster-android/go/app_interface"
)

type tunnelService struct {
	fd          int
	onEstablish func() error
}

func (s *tunnelService) CallFunction(name string, _ []byte) ([]byte, error) {
	switch name {
	case "VPNInit":
		if s.onEstablish != nil {
			if err := s.onEstablish(); err != nil {
				return nil, err
			}
		}
		return cbor.Marshal(s.fd)
	case "GetAppUID":
		return cbor.Marshal(10001)
	}
	return nil, errors.New("unexpected bridge call: " + name)
}

func tunnelPair(t *testing.T) (int, int) {
	t.Helper()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_DGRAM|syscall.SOCK_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { syscall.Close(fds[1]) })
	return fds[0], fds[1]
}

func TestFailedHandoffRetainsEstablishedTunnel(t *testing.T) {
	fd, _ := tunnelPair(t)
	service := &tunnelService{fd: fd}
	app_interface.SetServiceFunctions(service)
	t.Cleanup(app_interface.RemoveServiceFunctionReference)
	t.Cleanup(destroyTunnelInterface)
	if err := setupTunnelInterface(); err != nil {
		t.Fatal(err)
	}
	service.onEstablish = func() error {
		if !IsActive() {
			t.Error("old tunnel disappeared before replacement was established")
		}
		if _, err := syscall.Getsockname(fd); err != nil {
			t.Errorf("old descriptor closed: %v", err)
		}
		return errors.New("replacement unavailable")
	}
	if err := setupTunnelInterface(); err == nil {
		t.Fatal("expected replacement failure")
	}
	if !IsActive() {
		t.Fatal("failed handoff removed VPN protection")
	}
	if _, err := syscall.Getsockname(fd); err != nil {
		t.Fatal("failed handoff closed original descriptor")
	}
}

func TestSuccessfulHandoffClosesOldTunnelOnlyAfterEstablish(t *testing.T) {
	oldFD, _ := tunnelPair(t)
	newFD, _ := tunnelPair(t)
	service := &tunnelService{fd: oldFD}
	app_interface.SetServiceFunctions(service)
	t.Cleanup(app_interface.RemoveServiceFunctionReference)
	t.Cleanup(destroyTunnelInterface)
	if err := setupTunnelInterface(); err != nil {
		t.Fatal(err)
	}
	service.fd = newFD
	service.onEstablish = func() error {
		if !IsActive() {
			t.Error("VPN became inactive during handoff")
		}
		if _, err := syscall.Getsockname(oldFD); err != nil {
			t.Errorf("old descriptor closed early: %v", err)
		}
		return nil
	}
	if err := setupTunnelInterface(); err != nil {
		t.Fatal(err)
	}
	if !IsActive() {
		t.Fatal("new tunnel not active")
	}
	if _, err := syscall.Getsockname(oldFD); err == nil {
		t.Fatal("superseded descriptor leaked")
	}
}
