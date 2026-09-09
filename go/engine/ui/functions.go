package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/safing/portbase/config"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster-android/go/app_interface"
	"github.com/safing/portmaster-android/go/engine"
	"github.com/safing/portmaster-android/go/engine/bug_report"
	"github.com/safing/portmaster-android/go/engine/logs"
	"github.com/safing/portmaster-android/go/engine/tunnel"
	"github.com/safing/portmaster/profile"
	"github.com/safing/spn/access"
	"github.com/safing/spn/captain"
)

// Functions that have PluginCall as an argument are automatically exposed to the ionic UI

func IsTunnelActive() bool {
	return tunnel.IsActive()
}

func EnableTunnel() {
	// Send request to the VPN Service, with will notify the module.
	app_interface.SendServicesCommand("keep_alive")
}

func RestartTunnel() {
	tunnel.Reconnect()
}

// SPNLogin authenticates directly through the SPN access client. Keeping this
// flow inside Go avoids coupling account login to the legacy WebView HTTP bridge.
func SPNLogin(username, password string) (string, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return "", fmt.Errorf("username and password are required")
	}

	access.EnableAfterLogin = true
	user, code, err := access.Login(username, password)
	if err != nil {
		if code != 0 {
			return "", fmt.Errorf("SPN login failed (HTTP %d): %w", code, err)
		}
		return "", fmt.Errorf("SPN login failed: %w", err)
	}

	if user.MayUseTheSPN() {
		if err := config.SetConfigOption(captain.CfgOptionEnableSPNKey, true); err != nil {
			return "", fmt.Errorf("logged in, but failed to enable SPN: %w", err)
		}
	}

	data, err := json.Marshal(user)
	if err != nil {
		return "", fmt.Errorf("failed to encode SPN profile: %w", err)
	}
	return string(data), nil
}

func SPNLogout() error {
	return access.Logout(false, true)
}

func RefreshSPNUserProfile() (string, error) {
	user, _, err := access.UpdateUser()
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(user)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func GetSPNUserProfile() (string, error) {
	user, err := access.GetUser()
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(user)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func GetSPNStatus() (string, error) {
	data, err := json.Marshal(captain.GetSPNStatus())
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func SetSPNEnabled(enabled bool) error {
	return config.SetConfigOption(captain.CfgOptionEnableSPNKey, enabled)
}

// SetSPNExitCountry sets the global SPN exit-node policy. An empty country
// restores automatic routing. A country code forces exits to that country.
func SetSPNExitCountry(country string) error {
	country = strings.ToUpper(strings.TrimSpace(country))
	if country == "" {
		return config.SetConfigOption(profile.CfgOptionExitHubPolicyKey, []string{})
	}
	if len(country) != 2 || country[0] < 'A' || country[0] > 'Z' ||
		country[1] < 'A' || country[1] > 'Z' {
		return fmt.Errorf("invalid ISO country code %q", country)
	}
	return config.SetConfigOption(
		profile.CfgOptionExitHubPolicyKey,
		[]string{"+ " + country, "- *"},
	)
}

func GetSPNExitCountry() string {
	rules := config.GetAsStringArray(profile.CfgOptionExitHubPolicyKey, []string{})()
	for _, rule := range rules {
		fields := strings.Fields(rule)
		if len(fields) == 2 && fields[0] == "+" && len(fields[1]) == 2 {
			return strings.ToUpper(fields[1])
		}
	}
	return ""
}

func GetTunnelLastError() string {
	return tunnel.LastError()
}

func GetLogs(ID int64) []logs.LogLine {
	return logs.GetAllLogsAfterID(uint64(ID))
}

func GetDebugInfoFile() {
	log.Infof("engine: exporting debug info")
	debugInfo, err := logs.GetDebugInfo("github")
	if err != nil {
		return
	}
	_ = app_interface.ExportDebugInfo("PortmasterDebugInfo.txt", debugInfo)
}

func GetDebugInfo() (string, error) {
	debugInfo, err := logs.GetDebugInfo("github")
	escaped := strings.ReplaceAll(string(debugInfo), `"`, `\"`)
	return escaped, err
}

func Shutdown() {
	engine.OnDestroy()
}

func CreateIssue(debugInfo string, genUrl bool, issueRequestStr string) (string, error) {
	var issueRequest bug_report.IssueRequest
	err := json.Unmarshal([]byte(issueRequestStr), &issueRequest)
	if err != nil {
		return "", fmt.Errorf("failed to parse issueRequest object: %s", err)
	}

	// Upload debug info to private bin
	if debugInfo != "" {
		debugInfoUrl, err := bug_report.UploadToPrivateBin("debug-info", debugInfo)
		if err != nil {
			return "", fmt.Errorf("failed to upload debug info: %s", err)
		}
		issueRequest.DebugInfoUrl = debugInfoUrl
	}

	url, err := bug_report.CreateIssue(&issueRequest, "portmaster-android", "report-bug.md", genUrl)
	if err != nil {
		return "", fmt.Errorf("failed to create issue: %s", err)
	}
	return url, nil
}

func CreateTicket(debugInfo string, ticketRequestStr string) error {
	var ticketRequest bug_report.TicketRequest
	err := json.Unmarshal([]byte(ticketRequestStr), &ticketRequest)
	if err != nil {
		return fmt.Errorf("failed to parse ticketRequest object: %s", err)
	}

	// Upload debug info to private bin
	if debugInfo != "" {
		debugInfoUrl, err := bug_report.UploadToPrivateBin("debug-info", debugInfo)
		if err != nil {
			return fmt.Errorf("failed to upload debug info: %s", err)
		}
		ticketRequest.DebugInfoUrl = debugInfoUrl
	}

	return bug_report.CreateTicket(&ticketRequest)
}

func IsGeoIPDataAvailable() (bool, error) {
	return engine.IsGeoIPDataAvailable()
}

func NewApkAvaliable() bool {
	return engine.NewApkVersion.IsSet()
}

func PerformRequest(call PluginCall) {
	// Parameter requestJson.
	requestJson, err := call.GetString("requestJson")
	if err != nil {
		call.Error("missing requestJson argument")
		return
	}

	var request Request
	if err := json.Unmarshal([]byte(requestJson), &request); err != nil {
		log.Errorf("engine: failed to parse ui request: %s %q", err, requestJson)
		call.Error(err.Error())
		return
	}

	if !strings.HasPrefix(request.Url, "internal:") {
		log.Errorf("Path not implemented for: %s", request.Url)
		call.Error("Path not implemented")
		return
	}

	// The Angular UI still uses the legacy internal:/v1/... namespace.
	// Portbase 0.16+ exposes those endpoints under /api/v1/...
	internalPath := strings.TrimPrefix(request.Url, "internal:")
	if !strings.HasPrefix(internalPath, "/") {
		internalPath = "/" + internalPath
	}
	switch {
	case internalPath == "/v1":
		internalPath = "/api/v1"
	case strings.HasPrefix(internalPath, "/v1/"):
		internalPath = "/api" + internalPath
	}

	targetURL := engine.InternalAPIBaseURL() + internalPath
	client := &http.Client{Timeout: 30 * time.Second}
	var response *http.Response

	// During app startup the WebView can issue its first request just before
	// Portbase's API worker has bound the loopback socket. Rebuild the request
	// on every retry so POST/PUT bodies are never reused after a failed attempt.
	for attempt := 0; attempt < 10; attempt++ {
		httpRequest, requestErr := http.NewRequest(request.Method, targetURL, strings.NewReader(request.Body))
		if requestErr != nil {
			log.Errorf("engine: failed to create internal API request: %s", requestErr)
			call.Error(requestErr.Error())
			return
		}
		for key, values := range request.Headers {
			for _, value := range values {
				httpRequest.Header.Add(key, value)
			}
		}
		httpRequest.Header.Set(engine.InternalAPIAuthHeader, engine.InternalAPIToken())

		response, err = client.Do(httpRequest)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		log.Errorf("engine: internal API request failed: %s", err)
		call.Error(err.Error())
		return
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		log.Errorf("engine: failed to read internal API response: %s", err)
		call.Error(err.Error())
		return
	}

	log.Debugf("engine: internal API response: %s %d", internalPath, response.StatusCode)
	if response.StatusCode < http.StatusBadRequest {
		call.ResolveJson(fmt.Sprintf(`{"data": %q}`, string(body)))
		return
	}

	errorBody := strings.TrimSpace(string(body))
	if errorBody == "" {
		errorBody = response.Status
	}
	call.Error(errorBody)
}

func DatabaseMessage(msg string) {
	if err := databaseSendMessage(msg); err != nil {
		log.Errorf("ui: failed to send database message: %s", err)
	}
}

func SubscribeToDatabase(call PluginCall) {
	if err := ensureDatabaseBridge(); err != nil {
		call.Error(err.Error())
		return
	}

	call.KeepAlive(true)
	setDatabaseCall(call)
	call.Resolve()
}

func DownloadPendingUpdates() {
	engine.DownloadUpdates()
}

func DownloadUpdatesOnWifiConnected() {
	engine.DownloadUpdatesOnWifiConnected()
}

func IsOnWifiNetwork() bool {
	return engine.IsCurrentNetworkNotMetered.IsSet()
}
