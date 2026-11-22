// Package kinga provides a small standard-library client for the Kinga API.
package kinga

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	base          *url.URL
	keyID, tenant string
	secret        []byte
	http          *http.Client
}

func New(baseURL, tenant, keyID string, secret []byte, client *http.Client) (*Client, error) {
	base, err := url.Parse(baseURL)
	if err != nil || base.Scheme == "" || base.Host == "" || (base.Scheme != "https" && base.Scheme != "http") {
		return nil, fmt.Errorf("invalid Kinga base URL")
	}
	if tenant == "" || keyID == "" || len(secret) < 16 {
		return nil, fmt.Errorf("tenant, key ID and at least 16 secret bytes are required")
	}
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	return &Client{base: base, tenant: tenant, keyID: keyID, secret: append([]byte(nil), secret...), http: client}, nil
}

func (c *Client) Assess(ctx context.Context, tenantEventID string, event any) (json.RawMessage, error) {
	body, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	request, err := c.request(ctx, http.MethodPost, "/v1/assess", body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Idempotency-Key", tenantEventID)
	return c.send(request)
}

func (c *Client) Tokenize(ctx context.Context, namespace, value, scope string) (json.RawMessage, error) {
	body, err := json.Marshal(map[string]string{"namespace": namespace, "value": value, "scope": scope})
	if err != nil {
		return nil, err
	}
	request, err := c.request(ctx, http.MethodPost, "/v1/tokenize", body)
	if err != nil {
		return nil, err
	}
	return c.send(request)
}

func (c *Client) request(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.Contains(path, "..") {
		return nil, fmt.Errorf("API path must be absolute and stay within the Kinga host")
	}
	endpoint := *c.base
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + path
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, c.secret)
	_, _ = mac.Write([]byte(timestamp + "." + string(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Kinga-Key", c.keyID)
	request.Header.Set("X-Kinga-Tenant", c.tenant)
	request.Header.Set("X-Kinga-Timestamp", timestamp)
	request.Header.Set("X-Kinga-Signature", hex.EncodeToString(mac.Sum(nil)))
	return request, nil
}

func (c *Client) send(request *http.Request) (json.RawMessage, error) {
	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 1<<20 {
		return nil, fmt.Errorf("Kinga response too large")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Kinga API returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.RawMessage(body), nil
}
