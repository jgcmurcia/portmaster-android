package ui

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster-android/go/engine"
)

var (
	databaseBridgeMu      sync.Mutex
	databaseBridgeWriteMu sync.Mutex
	databaseCallMu        sync.RWMutex
	databaseBridgeConn    *websocket.Conn
)

var databaseReadPrefixes = []string{
	"config:",
	"runtime:spn/status",
	"runtime:core/updates/state",
	"runtime:system/status",
	"runtime:subsystems/",
	"runtime:system/security-level",
	"core:status/versions",
	"core:spn/account/user",
	"map:main/",
	"notifications:all/",
}

func setDatabaseCall(call PluginCall) {
	databaseCallMu.Lock()
	dbCall = call
	databaseCallMu.Unlock()
}

func getDatabaseCall() PluginCall {
	databaseCallMu.RLock()
	defer databaseCallMu.RUnlock()
	return dbCall
}

func databaseTargetAllowed(target string, prefixes []string) bool {
	target = strings.TrimSpace(target)
	for _, prefix := range prefixes {
		if strings.HasPrefix(target, prefix) {
			return true
		}
	}
	return false
}

// validateDatabaseMessage is a defense-in-depth boundary between the bundled
// WebView and Portbase's legacy admin-capable database WebSocket. The UI only
// needs a small subset of the database; SPN auth-token and other internal
// records are deliberately unreachable even if JavaScript is compromised.
func validateDatabaseMessage(msg string) error {
	parts := strings.SplitN(msg, "|", 4)
	if len(parts) < 2 || parts[0] == "" {
		return fmt.Errorf("malformed database bridge message")
	}

	method := parts[1]
	if method == "cancel" {
		if len(parts) != 2 {
			return fmt.Errorf("malformed database cancel message")
		}
		return nil
	}
	if len(parts) < 3 {
		return fmt.Errorf("database method %q is missing a target", method)
	}

	target := parts[2]
	switch method {
	case "query", "sub", "qsub":
		target = strings.TrimSpace(target)
		if strings.HasPrefix(target, "query ") {
			target = strings.TrimSpace(strings.TrimPrefix(target, "query "))
		}
		if !databaseTargetAllowed(target, databaseReadPrefixes) {
			return fmt.Errorf("database read target is not allowed")
		}
		return nil

	case "get":
		if !databaseTargetAllowed(target, databaseReadPrefixes) {
			return fmt.Errorf("database read target is not allowed")
		}
		return nil

	case "update":
		if strings.HasPrefix(target, "config:") ||
			target == "runtime:system/security-level" ||
			strings.HasPrefix(target, "notifications:all/") {
			return nil
		}
		return fmt.Errorf("database update target is not allowed")

	case "create", "delete":
		if strings.HasPrefix(target, "notifications:all/") {
			return nil
		}
		return fmt.Errorf("database write target is not allowed")

	case "insert":
		return fmt.Errorf("database insert is not exposed to the Android UI")

	default:
		return fmt.Errorf("database method %q is not allowed", method)
	}
}

func ensureDatabaseBridge() error {
	databaseBridgeMu.Lock()
	defer databaseBridgeMu.Unlock()

	if databaseBridgeConn != nil {
		return nil
	}

	baseURL := engine.InternalAPIBaseURL()
	if baseURL == "" {
		return fmt.Errorf("internal Portmaster API is not initialized")
	}
	wsURL := strings.Replace(baseURL, "http://", "ws://", 1) + "/api/database/v1"

	headers := http.Header{}
	headers.Set(engine.InternalAPIAuthHeader, engine.InternalAPIToken())

	var lastErr error
	for attempt := 0; attempt < 20; attempt++ {
		conn, response, err := websocket.DefaultDialer.Dial(wsURL, headers)
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		if err == nil {
			databaseBridgeConn = conn
			go databaseReadLoop(conn)
			return nil
		}

		lastErr = err
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("connect to Portmaster database API: %w", lastErr)
}

func databaseSendMessage(msg string) error {
	if err := validateDatabaseMessage(msg); err != nil {
		log.Warningf("ui: rejected database bridge request: %s", err)
		return err
	}

	if err := ensureDatabaseBridge(); err != nil {
		return err
	}

	databaseBridgeMu.Lock()
	conn := databaseBridgeConn
	databaseBridgeMu.Unlock()
	if conn == nil {
		return fmt.Errorf("Portmaster database API is not connected")
	}

	databaseBridgeWriteMu.Lock()
	err := conn.WriteMessage(websocket.BinaryMessage, []byte(msg))
	databaseBridgeWriteMu.Unlock()
	if err != nil {
		invalidateDatabaseBridge(conn)
		return fmt.Errorf("write Portmaster database API message: %w", err)
	}
	return nil
}

func databaseReadLoop(conn *websocket.Conn) {
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			invalidateDatabaseBridge(conn)
			log.Warningf("ui: Portmaster database API connection closed: %s", err)
			return
		}

		call := getDatabaseCall()
		if call == nil {
			continue
		}
		if err := call.Notify("db_event", fmt.Sprintf(`{"data": %q}`, string(data))); err != nil {
			log.Errorf("ui: failed to notify UI for database response: %s", err)
		}
	}
}

func invalidateDatabaseBridge(conn *websocket.Conn) {
	databaseBridgeMu.Lock()
	if databaseBridgeConn == conn {
		databaseBridgeConn = nil
	}
	databaseBridgeMu.Unlock()
	_ = conn.Close()
}
