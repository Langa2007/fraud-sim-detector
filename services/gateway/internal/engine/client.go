package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	endpoint string
	http     *http.Client
}

func NewClient(endpoint string, timeout time.Duration) *Client {
	return &Client{endpoint: endpoint, http: &http.Client{Timeout: timeout}}
}
func (c *Client) Assess(ctx context.Context, event Event, tenant string) (Result, error) {
	payload, err := json.Marshal(map[string]any{"tenant": tenant, "event": event})
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/internal/v1/assess", bytes.NewReader(payload))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("decision engine status %d", resp.StatusCode)
	}
	var result Result
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return Result{}, err
	}
	if result.Mode != "shadow" || result.TenantEventID != event.TenantEventID {
		return Result{}, fmt.Errorf("decision engine returned invalid contract")
	}
	return result, nil
}
func (c *Client) Ready(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("decision engine not ready: status %d", resp.StatusCode)
	}
	return nil
}
