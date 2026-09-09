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
