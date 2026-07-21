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
}

func NewClient(serverURL string) *Client {
	return &Client{
		ServerURL:  serverURL,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
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
