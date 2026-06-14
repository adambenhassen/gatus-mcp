// Package gatus is a thin client for Gatus's REST API.
//
// It exposes just the read endpoints the MCP tools need plus the single
// external-result write, turning connection failures and non-2xx responses into
// descriptive errors.
package gatus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client is a read/write wrapper over a single Gatus instance's REST API.
type Client struct {
	http    *http.Client
	baseURL string
	token   string
}

// NewClient builds a client for the given Gatus base URL. token may be empty; it
// is only required by PostExternal.
func NewClient(baseURL, token string) *Client {
	return &Client{
		http:    &http.Client{Timeout: 15 * time.Second},
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
	}
}

// HasToken reports whether a bearer token was configured.
func (c *Client) HasToken() bool {
	return c.token != ""
}

// ConditionResult mirrors a single condition evaluation in a probe result.
type ConditionResult struct {
	Condition string `json:"condition"`
	Success   bool   `json:"success"`
}

// Result mirrors a single probe result. Duration is in nanoseconds.
type Result struct {
	Timestamp        string            `json:"timestamp"`
	ConditionResults []ConditionResult `json:"conditionResults"`
	Errors           []string          `json:"errors"`
	Duration         int64             `json:"duration"`
	Status           int               `json:"status"`
	Success          bool              `json:"success"`
}

// Endpoint mirrors an endpoint entry from /api/v1/endpoints/statuses.
type Endpoint struct {
	Name    string   `json:"name"`
	Group   string   `json:"group"`
	Key     string   `json:"key"`
	Results []Result `json:"results"`
}

// do issues a request and returns the body, turning connection failures and
// non-2xx responses into descriptive errors (the equivalent of Python's
// raise_for_status, with a clearer message for the model).
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body io.Reader) (_ []byte, err error) {
	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gatus request failed: %w (%s %s)", err, method, path)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("gatus request failed: closing body: %w (%s %s)", cerr, method, path)
		}
	}()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gatus request failed: reading body: %w (%s %s)", err, method, path)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("gatus request failed: %s for %s %s: %s",
			resp.Status, method, path, strings.TrimSpace(string(data)))
	}
	return data, nil
}

// GetJSON fetches path and decodes the JSON body into out.
func (c *Client) GetJSON(ctx context.Context, path string, query url.Values, out any) error {
	data, err := c.do(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("gatus returned invalid JSON for %s: %w", path, err)
	}
	return nil
}

// GetText fetches path and returns the raw body (used for raw numbers and SVG).
func (c *Client) GetText(ctx context.Context, path string) (string, error) {
	data, err := c.do(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// FetchStatuses returns the full snapshot of all endpoints.
func (c *Client) FetchStatuses(ctx context.Context) ([]Endpoint, error) {
	var endpoints []Endpoint
	if err := c.GetJSON(ctx, "/api/v1/endpoints/statuses", nil, &endpoints); err != nil {
		return nil, err
	}
	return endpoints, nil
}

// PostExternal submits an external endpoint result (a write). It requires a
// bearer token; see HasToken.
func (c *Client) PostExternal(ctx context.Context, key string, success bool, errMsg, duration string) (string, error) {
	q := url.Values{}
	q.Set("success", strconv.FormatBool(success))
	if errMsg != "" {
		q.Set("error", errMsg)
	}
	if duration != "" {
		q.Set("duration", duration)
	}
	data, err := c.do(ctx, http.MethodPost, "/api/v1/endpoints/"+url.PathEscape(key)+"/external", q, http.NoBody)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
