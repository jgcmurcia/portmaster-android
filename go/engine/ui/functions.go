package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

const (
	maxInternalRequestJSON = 2 << 20
	maxInternalRequestBody = 1 << 20
	maxInternalResponse    = 2 << 20
)

var allowedInternalPostPaths = map[string]struct{}{
	"/api/v1/core/restart":           {},
	"/api/v1/core/shutdown":          {},
	"/api/v1/updates/check":          {},
	"/api/v1/ui/reload":              {},
	"/api/v1/dns/clear":              {},
	"/api/v1/broadcasts/reset-state": {},
	"/api/v1/spn/reinit":             {},
}

// Functions that have PluginCall as an argument are automatically exposed to the ionic UI

func IsTunnelActive() bool {
	return tunnel.IsActive()
}

func EnableTunnel() {
	if err := app_interface.SendServicesCommand("keep_alive"); err != nil {
		log.Errorf("ui: failed to start VPN service: %s", err)
	}
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
		log.Errorf("ui: failed to build debug info: %s", err)
		return
	}
	if err := app_interface.ExportDebugInfo("PortmasterDebugInfo.txt", debugInfo); err != nil {
		log.Errorf("ui: failed to export debug info: %s", err)
	}
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

	if debugInfo != "" {
		debugInfoURL, err := bug_report.UploadToPrivateBin("debug-info", debugInfo)
		if err != nil {
			return "", fmt.Errorf("failed to upload debug info: %s", err)
		}
		issueRequest.DebugInfoUrl = debugInfoURL
	}

	issueURL, err := bug_report.CreateIssue(&issueRequest, "portmaster-android", "report-bug.md", genUrl)
	if err != nil {
		return "", fmt.Errorf("failed to create issue: %s", err)
	}
	return issueURL, nil
}

func CreateTicket(debugInfo string, ticketRequestStr string) error {
	var ticketRequest bug_report.TicketRequest
	err := json.Unmarshal([]byte(ticketRequestStr), &ticketRequest)
	if err != nil {
		return fmt.Errorf("failed to parse ticketRequest object: %s", err)
	}

	if debugInfo != "" {
		debugInfoURL, err := bug_report.UploadToPrivateBin("debug-info", debugInfo)
		if err != nil {
			return fmt.Errorf("failed to upload debug info: %s", err)
		}
		ticketRequest.DebugInfoUrl = debugInfoURL
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
	requestJSON, err := call.GetString("requestJson")
	if err != nil {
		call.Error("missing requestJson argument")
		return
	}
	if len(requestJSON) > maxInternalRequestJSON {
		call.Error("internal API request is too large")
		return
	}

	var request Request
	if err := json.Unmarshal([]byte(requestJSON), &request); err != nil {
		// Never log the raw request. It can contain sensitive configuration data.
		log.Errorf("engine: failed to parse ui request: %s", err)
		call.Error("invalid internal API request")
		return
	}

	if len(request.Body) > maxInternalRequestBody {
		call.Error("internal API request body is too large")
		return
	}
	if !strings.HasPrefix(request.Url, "internal:") {
		call.Error("internal API path required")
		return
	}

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

	parsedPath, err := url.ParseRequestURI(internalPath)
	if err != nil || parsedPath.IsAbs() || parsedPath.Host != "" {
		call.Error("invalid internal API path")
		return
	}
	if strings.ToUpper(request.Method) != http.MethodPost {
		call.Error("internal API method is not allowed")
		return
	}
	if _, ok := allowedInternalPostPaths[parsedPath.Path]; !ok {
		log.Warningf("ui: blocked legacy internal API path %s", parsedPath.Path)
		call.Error("internal API path is not allowed")
		return
	}

	targetURL := engine.InternalAPIBaseURL() + parsedPath.RequestURI()
	client := &http.Client{Timeout: 30 * time.Second}
	var response *http.Response

	for attempt := 0; attempt < 10; attempt++ {
		httpRequest, requestErr := http.NewRequest(http.MethodPost, targetURL, strings.NewReader(request.Body))
		if requestErr != nil {
			call.Error("failed to create internal API request")
			return
		}

		// Do not forward arbitrary WebView-controlled hop-by-hop/auth headers.
		for _, key := range []string{"Accept", "Content-Type"} {
			for _, value := range request.Headers[key] {
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
		call.Error("internal API request failed")
		return
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxInternalResponse+1))
	if err != nil {
		call.Error("failed to read internal API response")
		return
	}
	if len(body) > maxInternalResponse {
		call.Error("internal API response is too large")
		return
	}

	log.Debugf("engine: internal API response: %s %d", parsedPath.Path, response.StatusCode)
	if response.StatusCode < http.StatusBadRequest {
		call.ResolveJson(fmt.Sprintf(`{"data": %q}`, string(body)))
		return
	}

	errorBody := strings.TrimSpace(string(body))
	if errorBody == "" {
		errorBody = response.Status
	}
	if len(errorBody) > 2048 {
		errorBody = errorBody[:2048]
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
