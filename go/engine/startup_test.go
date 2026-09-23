package engine

import (
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/safing/portbase/modules"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/safing/portbase/config"
	"github.com/safing/portbase/database"
	"github.com/safing/portbase/database/record"
	"github.com/safing/portmaster-android/go/app_interface"
	"github.com/safing/portmaster-android/go/engine/logs"
	"github.com/safing/portmaster-android/go/engine/tunnel"
	"github.com/safing/spn/captain"
)

type startupPlatform struct{ fd int }
type startupRecord struct {
	record.Base
	sync.Mutex
	Value string
}

func (p startupPlatform) CallFunction(name string, args []byte) ([]byte, error) {
	switch name {
	case "VPNInit":
		return cbor.Marshal(p.fd)
	case "GetAppUID":
		return cbor.Marshal(10001)
	case "SendServiceCommand":
		return nil, nil
	case "GetPlatformInfo":
		return cbor.Marshal(app_interface.PlatformInfo{VersionName: "1.0.0", ApplicationID: StableApplicationID, BuildType: "debug", SDK: 32})
	case "GetNetworkInterfaces", "GetNetworkAddresses":
		return cbor.Marshal([]any{})
	}
	return nil, fmt.Errorf("unsupported platform call %s", name)
}

// The legacy module registry cannot restart. Run the actual Android entry point
// in a separate process so other tests never inherit its global state.
func TestAndroidColdStartInitializesDatabase(t *testing.T) {
	if os.Getenv("PORTMASTER_STARTUP_CHILD") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestAndroidColdStartInitializesDatabase$", "-test.timeout=75s")
		cmd.Env = append(os.Environ(), "PORTMASTER_STARTUP_CHILD=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Android cold start failed: %v\n%s", err, out)
		}
		return
	}
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_DGRAM|syscall.SOCK_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fds[1])
	platform := startupPlatform{fd: fds[0]}
	SetOSFunctions(platform)
	SetServiceFunctions(platform)
	OnCreate(t.TempDir())
	defer modules.Shutdown()
	if err := WaitForReady(); err != nil {
		t.Fatalf("startup failed: %v", err)
	}
	headers := http.Header{}
	headers.Set(InternalAPIAuthHeader, InternalAPIToken())
	dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
	conn, response, err := dialer.Dial(strings.Replace(InternalAPIBaseURL(), "http://", "ws://", 1)+"/api/database/v1", headers)
	if err != nil {
		t.Fatalf("internal database bridge unavailable: %v (response %v)", err, response)
	}
	conn.Close()

	db := database.NewInterface(nil)
	saved := &startupRecord{Value: "profile-persistence-probe"}
	saved.SetKey("core:startup-test/profile")
	if err := db.Put(saved); err != nil {
		t.Fatalf("cannot save profile: %v", err)
	}
	loaded, err := db.Get(saved.Key())
	if err != nil {
		t.Fatal(err)
	}
	value, ok := loaded.GetAccessor(loaded).GetString("Value")
	if !ok || value != saved.Value {
		t.Fatalf("profile round trip failed: %q", value)
	}
	tunnel.Enable()
	deadline := time.Now().Add(5 * time.Second)
	for !tunnel.IsActive() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !tunnel.IsActive() {
		t.Fatalf("VPN manager did not establish tunnel: %s", tunnel.LastError())
	}
	tunnel.Disable()
	// No account credentials are used: this checks resource/module startup,
	// not an authenticated SPN connection.
	if err := config.SetConfigOption(captain.CfgOptionEnableSPNKey, true); err != nil {
		t.Fatal(err)
	}
	spnDeadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(spnDeadline) {
		if captain.GetSPNStatus().Status != captain.StatusDisabled {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("SPN client manager never started; logs: %+v", logs.GetAllLogsAfterID(0))
}
