package engine

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

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
	if engineInitialized.IsSet() {
		fmt.Println("engine: was already initialized")
		return
	}
	engineInitialized.Set()

	platformInfo, err := app_interface.GetPlatformInfo()
	if err != nil || platformInfo == nil {
		fmt.Printf("engine: failed to get Android platform info: %v\n", err)
		engineInitialized.UnSet()
		return
	}

	info.Set("PortmasterAndroid", platformInfo.VersionName, "AGPLv3", true)
	log.SetAdapter(logs.GetLogFunc())

	fmt.Println("engine: initializing...")
	fmt.Printf("%s %s %s\n", info.GetInfo().Name, info.Version(), info.GetInfo().BuildDate)

	dataDir = appDir
	if dataDir == "" {
		log.Error("engine: Android data directory is empty")
		engineInitialized.UnSet()
		return
	}

	// Portbase 0.16+ routes API requests through its HTTP/WebSocket router.
	// Keep it on an ephemeral loopback-only port and require a per-process
	// random token for protected endpoints. This lets Android use the supported
	// API path without exposing Portmaster's administrative API to other apps.
	if err := configureInternalAPI(); err != nil {
		log.Errorf("engine: failed to configure internal API: %s", err)
		engineInitialized.UnSet()
		return
	}

	conf.EnableClient(true)
	sluice.EnableListener = false

	// Android handles APK updates externally, but the SPN Intel data still needs
	// the Portmaster updater. Keep software updates disabled while retaining Intel.
	updates.DisableSoftwareAutoUpdate = true
	if err := updates.DisableUpdateSchedule(); err != nil {
		log.Warningf("engine: failed to disable periodic update schedule: %s", err)
	}
	helper.IntelOnly()

	access.EnableAfterLogin = true

	if err := dataroot.Initialize(dataDir, 0o0755); err != nil {
		log.Errorf("engine: failed to initialize dataroot: %s", err)
		engineInitialized.UnSet()
		return
	}
	dataRoot = dataroot.Root()
	if dataRoot == nil {
		log.Error("engine: dataroot initialized without a root directory")
		engineInitialized.UnSet()
		return
	}
	if err := logs.EnsureLoggingDir(dataRoot); err != nil {
		log.Errorf("engine: failed to initialize logging directory: %s", err)
		engineInitialized.UnSet()
		return
	}

	if platformInfo.BuildType == "debug" {
		log.SetLogLevel(log.TraceLevel)
	}
	logs.InitLogs()

	go func() {
		exitCode := run.Run()
		if exitCode != 0 {
			log.Errorf("engine: Portmaster module system stopped with exit code %d", exitCode)
		}
	}()

	// Android disables Portmaster's periodic software update scheduler because
	// APK updates are handled by Android. Intel data is different: SPN cannot
	// bootstrap without current map/GeoIP resources. Trigger one update as soon
	// as the updates module is ready, retrying briefly during cold start.
	go func() {
		var lastErr error
		for attempt := 0; attempt < 15; attempt++ {
			time.Sleep(2 * time.Second)
			if err := updates.TriggerUpdate(true); err == nil {
				log.Info("engine: initial Android intel update triggered")
				return
			} else {
				lastErr = err
			}
		}
		log.Warningf("engine: updates module did not become ready for initial intel refresh: %v", lastErr)
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

func OnDestroy() {
	log.Info("engine: OnDestroy")

	if err := app_interface.MinimizeApp(); err != nil {
		log.Errorf("engine: %s", err.Error())
	}

	if err := modules.Shutdown(); err != nil {
		log.Errorf("failed to shutdown database: %s", err)
	}
	logs.FinalizeLog()
	engineInitialized.UnSet()

	// Full process termination is retained for this legacy core because the
	// module registry is not restartable in-process after modules.Shutdown().
	// It is invoked only after graceful module/TUN teardown has completed.
	if err := app_interface.Shutdown(); err != nil {
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
	if !app_interface.HasServiceFunctions() || !tunnel.IsActive() {
		OnDestroy()
	}
}

func SetServiceFunctions(functions app_interface.AppInterface) {
	app_interface.SetServiceFunctions(functions)
}

// OnServiceStop is called for an intentional Android service shutdown.
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
