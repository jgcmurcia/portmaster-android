package bug_report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"github.com/safing/portbase/log"
)

const (
	maxDebugUploadBytes = 8 << 20
	maxSupportReply     = 1 << 20
	supportTimeout      = 30 * time.Second
)

var supportHTTPClient = &http.Client{Timeout: supportTimeout}

type PrivateBin struct {
	Urls struct {
		File []string
	}
}

type Section struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type IssueRequest struct {
	Title        string    `json:"title"`
	Sections     []Section `json:"sections"`
	DebugInfoUrl string    `json:"debugInfoUrl"`
}

type TicketRequest struct {
	Title        string    `json:"title"`
	Sections     []Section `json:"sections"`
	RepoName     string    `json:"repoName"`
	Email        string    `json:"email"`
	DebugInfoUrl string    `json:"debugInfoUrl"`
}

func readSupportResponse(resp *http.Response) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSupportReply+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSupportReply {
		return nil, fmt.Errorf("support server response exceeds %d bytes", maxSupportReply)
	}
	return data, nil
}

// UploadToPrivateBin uploads diagnostics only when explicitly called by the UI.
func UploadToPrivateBin(file, content string) (string, error) {
	if len(content) > maxDebugUploadBytes {
		return "", fmt.Errorf("debug upload exceeds %d bytes", maxDebugUploadBytes)
	}

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)

	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, file))
	h.Set("Content-Type", "text/plain; charset=utf-8")

	part, err := writer.CreatePart(h)
	if err != nil {
		return "", fmt.Errorf("failed to create upload part: %w", err)
	}
	if _, err = part.Write([]byte(content)); err != nil {
		return "", fmt.Errorf("failed to write upload content: %w", err)
	}
	if err = writer.Close(); err != nil {
		return "", fmt.Errorf("failed to close upload body: %w", err)
	}

	httpRequest, err := http.NewRequest(http.MethodPost, "https://support.safing.io/api/v1/upload", body)
	if err != nil {
		return "", fmt.Errorf("failed to create upload request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", writer.FormDataContentType())

	response, err := supportHTTPClient.Do(httpRequest)
	if err != nil {
		return "", fmt.Errorf("support upload failed: %w", err)
	}
	defer response.Body.Close()

	bodyContent, err := readSupportResponse(response)
	if err != nil {
		return "", fmt.Errorf("failed to read upload response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("support upload returned HTTP %d", response.StatusCode)
	}

	bin := &PrivateBin{}
	if err = json.Unmarshal(bodyContent, bin); err != nil {
		return "", fmt.Errorf("failed to parse upload response: %w", err)
	}
	if len(bin.Urls.File) == 0 || !strings.HasPrefix(bin.Urls.File[0], "https://") {
		return "", fmt.Errorf("support upload returned no valid HTTPS file URL")
	}

	log.Info("bug-report: debug info uploaded successfully")
	return bin.Urls.File[0], nil
}

func CreateIssue(issueRequest *IssueRequest, repo string, preset string, genURL bool) (string, error) {
	body, err := json.Marshal(issueRequest)
	if err != nil {
		return "", fmt.Errorf("failed to serialize issue: %w", err)
	}
	// Do not log body: it can contain diagnostic URLs and user-entered data.
	log.Info("bug-report: sending issue request")

	req, err := http.NewRequest(
		http.MethodPost,
		fmt.Sprintf("https://support.safing.io/api/v1/issues/%s/%s", repo, preset),
		bytes.NewReader(body),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create issue request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if genURL {
		q := req.URL.Query()
		q.Add("generate-url", "")
		req.URL.RawQuery = q.Encode()
	}

	resp, err := supportHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send issue request: %w", err)
	}
	defer resp.Body.Close()

	respJSON, err := readSupportResponse(resp)
	if err != nil {
		return "", fmt.Errorf("failed to read issue response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("support issue endpoint returned HTTP %d", resp.StatusCode)
	}

	respMap := make(map[string]string)
	if err = json.Unmarshal(respJSON, &respMap); err != nil {
		return "", fmt.Errorf("failed to parse issue response: %w", err)
	}
	resultURL := respMap["url"]
	if resultURL != "" && !strings.HasPrefix(resultURL, "https://") {
		return "", fmt.Errorf("support endpoint returned a non-HTTPS issue URL")
	}
	return resultURL, nil
}

func CreateTicket(ticketRequest *TicketRequest) error {
	body, err := json.Marshal(ticketRequest)
	if err != nil {
		return fmt.Errorf("failed to serialize ticket: %w", err)
	}
	// Never log ticket JSON: it includes the user's email and report content.
	log.Info("bug-report: sending support ticket")

	req, err := http.NewRequest(http.MethodPost, "https://support.safing.io/api/v1/ticket", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create ticket request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := supportHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send ticket request: %w", err)
	}
	defer resp.Body.Close()

	_, readErr := readSupportResponse(resp)
	if readErr != nil {
		return fmt.Errorf("failed to read ticket response: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("support ticket endpoint returned HTTP %d", resp.StatusCode)
	}
	return nil
}
