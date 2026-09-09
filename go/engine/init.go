package engine

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/safing/portbase/api"
	_ "github.com/safing/portbase/database/storage/bbolt"
	"github.com/safing/portbase/dataroot"
	"github.com/safing/portbase/info"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	_ "github.com/safing/portbase/rng"
	"github.com/safing/portbase/run"
	"github.com/safing/portbase/utils"
	"github.com/tevino/abool"

	"github.com/safing/portmaster-android/go/app_interface"
	"github.com/safing/portmaster-android/go/engine/logs"
	"github.com/safing/portmaster-android/go/engine/tunnel"
	_ "github.com/safing/portmaster/network"
	"github.com/safing/portmaster/updates"
	"github.com/safing/portmaster/updates/helper"
	"github.com/safing/spn/access"
	_ "github.com/safing/spn/captain"
	"github.com/safing/spn/conf"
	"github.com/safing/spn/sluice"
)

const (
	StableApplicationID = "io.safing.portmaster.android"
	BetaApplicationID   = "io.safing.portmaster.android.beta"

	// InternalAPIAuthHeader authenticates the embedded WebView with the local
	// Portmaster API. The value is generated randomly for every process.
	InternalAPIAuthHeader = "X-Portmaster-Android-Token"
)

var (
	dataDir  string
	dataRoot *utils.DirStructure

	engineInitialized abool.AtomicBool

	internalAPIOnce    sync.Once
	internalAPIErr     error
	internalAPIBaseURL string
	internalAPIToken   string
)

func OnCreate(appDir string) {
	// Check if engine is already initialized.
	if engineInitialized.IsSet() {
		fmt.Println("engine: was already initialized")
		return
	}

	engineInitialized.Set()

	platformInfo, err := app_interface.GetPlatformInfo()
	info.Set("PortmasterAndroid", platformInfo.VersionName, "AGPLv3", true)
	log.SetAdapter(logs.GetLogFunc())

	fmt.Println("engine: initializing...")
	fmt.Printf("%s %s %s\n", info.GetInfo().Name, info.Version(), info.GetInfo().BuildDate)

	// Get application data dir. Were the application has access to write and read.
	dataDir = appDir

	// Portbase 0.16+ routes API requests through its HTTP/WebSocket router.
	// Keep it on an ephemeral loopback-only port and require a per-process
	// random token for protected endpoints. This lets Android use the supported
	// API path without exposing Portmaster's administrative API to other apps.
	if err := configureInternalAPI(); err != nil {
		log.Errorf("engine: failed to configure internal API: %s", err)
		engineInitialized.UnSet()
		return
	}

	// Enable SPN client.
	conf.EnableClient(true)
	// Disable SPN listeners.
	sluice.EnableListener = false

	// Disables auto update for large files. Small files will still be auto downloaded. (filter lists)
	updates.DisableSoftwareAutoUpdate = true
	updates.DisableUpdateSchedule()
	helper.IntelOnly()

	// Android must behave like desktop Portmaster: after a successful login,
	// automatically enable SPN when the account includes the SPN feature.
	access.EnableAfterLogin = true

	// Initialize database.
	err = dataroot.Initialize(dataDir, 0o0755)
	if err != nil {
		_ = fmt.Errorf("engine: failed to initialize dataroot: %s", err)
		return
	}
	dataRoot = dataroot.Root()
	err = logs.EnsureLoggingDir(dataRoot)
	if err != nil {
		_ = fmt.Errorf("engine: %s", err)
	}

	// Setup logs
	if platformInfo.BuildType == "debug" {
		log.SetLogLevel(log.TraceLevel)
	}
	logs.InitLogs()

	// Run the SPN service and all dependencies.
	go func() {
		_ = run.Run()
	}()

	// Android disables Portmaster's periodic software update scheduler because
	// APK updates are handled by Android. Intel data is different: SPN cannot
	// bootstrap without current map/GeoIP resources. Trigger one update as soon
	// as the updates module is ready, retrying briefly during cold start.
	go func() {
		for attempt := 0; attempt < 15; attempt++ {
			time.Sleep(2 * time.Second)
			if err := updates.TriggerUpdate(true); err == nil {
				log.Info("engine: initial Android intel update triggered")
				return
			}
		}
		log.Warning("engine: updates module did not become ready for initial intel refresh")
	}()
}

// InternalAPIBaseURL returns the process-local Portmaster HTTP endpoint.
func InternalAPIBaseURL() string {
	return internalAPIBaseURL
}

// InternalAPIToken returns the per-process secret used by the Android bridge.
func InternalAPIToken() string {
	return internalAPIToken
}

func configureInternalAPI() error {
	internalAPIOnce.Do(func() {
		// Reserve an available loopback port. Closing the probe listener before
		// Portbase starts has a tiny race window, but avoids a fixed global port
		// and greatly reduces conflicts with other Android applications.
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			internalAPIErr = fmt.Errorf("allocate loopback API port: %w", err)
			return
		}
		address := listener.Addr().String()
		if err := listener.Close(); err != nil {
			internalAPIErr = fmt.Errorf("release loopback API probe: %w", err)
			return
		}

		tokenBytes := make([]byte, 32)
		if _, err := rand.Read(tokenBytes); err != nil {
			internalAPIErr = fmt.Errorf("generate internal API token: %w", err)
			return
		}
		internalAPIToken = hex.EncodeToString(tokenBytes)
		internalAPIBaseURL = "http://" + address

		api.EnableServer = true
		api.SetDefaultAPIListenAddress(address)
		internalAPIErr = api.SetAuthenticator(func(r *http.Request, _ *http.Server) (*api.AuthToken, error) {
			supplied := r.Header.Get(InternalAPIAuthHeader)
			if subtle.ConstantTimeCompare([]byte(supplied), []byte(internalAPIToken)) != 1 {
				return nil, fmt.Errorf("Portmaster Android internal API authentication failed: %w", api.ErrAPIAccessDeniedMessage)
			}
			return &api.AuthToken{
				Read:  api.PermitSelf,
				Write: api.PermitSelf,
			}, nil
		})
	})
	return internalAPIErr
}

// OnDestroy shutdown module system and calls System.exit(0)
func OnDestroy() {
	log.Info("engine: OnDestroy")

	err := app_interface.MinimizeApp()
	if err != nil {
		log.Errorf("engine: %s", err.Error())
	}

	err = modules.Shutdown()
	if err != nil {
		log.Errorf("failed to shutdown database: %s", err)
	}
	logs.FinalizeLog()
	engineInitialized.UnSet()

	// Call exit(0) form java so the jvm knows whats happening.
	err = app_interface.Shutdown()
	if err != nil {
		fmt.Printf("engine: failed to shutdown app: %s", err.Error())
	}
}

func IsEngineInitialized() bool {
	return engineInitialized.IsSet()
}

func SetOSFunctions(functions app_interface.AppInterface) {
	app_interface.SetOSFunctions(functions)
}

func SetActivityFunctions(functions app_interface.AppInterface) {
	app_interface.SetActivityFunctions(functions)
}

func OnActivityDestroy() {
	app_interface.RemoveActivityFunctionReference()
	// CancelAllUISubscriptions()
	if !app_interface.HasServiceFunctions() || !tunnel.IsActive() {
		OnDestroy()
	}
}

func SetServiceFunctions(functions app_interface.AppInterface) {
	app_interface.SetServiceFunctions(functions)
}

// OnServiceStop is called for an intentional Android service shutdown.
// The tunnel has already been torn down by the vpn-service manager, so only
// remove the Java service reference. Treating this as a system failure causes
// the old shutdown path to recurse and terminate the whole Android process.
func OnServiceStop() {
	app_interface.RemoveServiceFunctionReference()
}

func OnServiceDestroy() {
	app_interface.RemoveServiceFunctionReference()
	tunnel.SystemShutdown()
	if !app_interface.HasActivityFunctions() {
		OnDestroy()
	}
}
