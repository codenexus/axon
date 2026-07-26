package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	ServerURL  string
	HTTPClient *http.Client
	// uploadHTTPClient has no timeout, unlike HTTPClient's blanket 15s —
	// that's fine for small JSON enroll/heartbeat calls but would abort a
	// multi-GB backup upload partway through. Kept as a separate client
	// entirely rather than raising HTTPClient's timeout, so a hung
	// heartbeat still fails fast.
	uploadHTTPClient *http.Client
}

func NewClient(serverURL string) *Client {
	return &Client{
		ServerURL:        serverURL,
		HTTPClient:       &http.Client{Timeout: 15 * time.Second},
		uploadHTTPClient: &http.Client{},
	}
}

func (c *Client) Enroll(token string, req EnrollRequest) (*EnrollResponse, error) {
	var resp EnrollResponse
	if err := c.post("/api/v1/enroll", token, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) Heartbeat(deviceCredential string, req HeartbeatRequest) (*HeartbeatResponse, error) {
	var resp HeartbeatResponse
	if err := c.post("/api/v1/heartbeat", deviceCredential, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// PushBackup streams a backup archive's bytes to Panel on request (see the
// backup package doc / CLAUDE.md's push-backup design: Pulse's own disk is
// the source of truth, Panel only holds the file transiently for a
// download). r is streamed directly into the request body — callers should
// pass an *os.File, not something that buffers the whole archive in
// memory.
func (c *Client) PushBackup(deviceCredential, backupID, instanceID string, r io.Reader, size int64) error {
	httpReq, err := http.NewRequest(http.MethodPost, c.ServerURL+"/api/v1/backups/"+backupID+"/upload", r)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	httpReq.ContentLength = size
	httpReq.Header.Set("Content-Type", "application/gzip")
	httpReq.Header.Set("Authorization", "Bearer "+deviceCredential)
	httpReq.Header.Set("X-Axon-Instance-Id", instanceID)

	resp, err := c.uploadHTTPClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("push backup %s: %w", backupID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("push backup %s returned %d: %s", backupID, resp.StatusCode, string(respBody))
	}
	return nil
}

// PullFileUpload fetches a held upload's bytes from Panel, in response to
// an upload_file command — the reversed direction of PushBackup, but the
// same principle: Pulse is always the side that dials out, never Panel.
// Caller must Close() the returned body. Uses uploadHTTPClient (no
// timeout) for the same reason PushBackup does — a large plugin/mod file
// must not be aborted by HTTPClient's blanket 15s.
func (c *Client) PullFileUpload(deviceCredential, holdingID, instanceID string) (io.ReadCloser, error) {
	httpReq, err := http.NewRequest(http.MethodGet, c.ServerURL+"/api/v1/files/"+holdingID+"/download", nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+deviceCredential)
	httpReq.Header.Set("X-Axon-Instance-Id", instanceID)

	resp, err := c.uploadHTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("pull file upload %s: %w", holdingID, err)
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("pull file upload %s returned %d: %s", holdingID, resp.StatusCode, string(respBody))
	}
	return resp.Body, nil
}

func (c *Client) post(path, bearer string, body any, out any) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, c.ServerURL+path, bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		httpReq.Header.Set("Authorization", "Bearer "+bearer)
	}

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("request %s: %w", path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s returned %d: %s", path, resp.StatusCode, string(respBody))
	}

	if out == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
