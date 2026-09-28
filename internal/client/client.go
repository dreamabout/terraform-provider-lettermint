// Package client is a small HTTP client for the Lettermint Team API
// (https://api.lettermint.co/v1, OpenAPI 0.0.1). It covers only what the
// provider needs.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseURL is the Team API's base URL.
const DefaultBaseURL = "https://api.lettermint.co/v1"

// Client talks to the Lettermint Team API with a Team API token.
type Client struct {
	baseURL   string
	token     string
	userAgent string
	http      *http.Client
}

// New returns a client for baseURL, authenticated with a Team API token
// (lm_team_…).
func New(baseURL, token, userAgent string) *Client {
	return &Client{
		baseURL:   strings.TrimRight(baseURL, "/"),
		token:     token,
		userAgent: userAgent,
		http:      &http.Client{Timeout: 60 * time.Second},
	}
}

// APIError is a non-2xx response from the API.
type APIError struct {
	StatusCode int
	Message    string
	// Errors holds field validation errors from a 422 response.
	Errors map[string][]string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("lettermint: HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("lettermint: HTTP %d: %s", e.StatusCode, e.Message)
}

// IsNotFound reports whether err is a 404 from the API.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

// Ping checks that the API is reachable and the token is accepted.
func (c *Client) Ping(ctx context.Context) error {
	return c.Do(ctx, http.MethodGet, "/ping", nil, nil)
}

// Do sends a request to path (relative to the base URL) with in as JSON body,
// and decodes a JSON response into out. in and out may be nil. A body wrapped
// as {"data": {...}} is unwrapped, since OpenAPI 0.0.1 is not consistent about
// which responses are wrapped.
func (c *Client) Do(ctx context.Context, method, path string, in, out any) error {
	status, body, err := c.send(ctx, method, path, in)
	if err != nil {
		return err
	}
	if status < 200 || status > 299 {
		return newAPIError(status, body)
	}
	return decode(method, path, body, out)
}

func decode(method, path string, body []byte, out any) error {
	if out == nil || len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(unwrapData(body), out); err != nil {
		return fmt.Errorf("lettermint: decode %s %s: %w", method, path, err)
	}
	return nil
}

// unwrapData returns the object under "data" when body is an object whose
// only resource is that key; otherwise body itself.
func unwrapData(body []byte) []byte {
	var top map[string]json.RawMessage
	if json.Unmarshal(body, &top) != nil {
		return body
	}
	data, ok := top["data"]
	if !ok {
		return body
	}
	if _, hasID := top["id"]; hasID {
		return body
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return body
	}
	return data
}

// send performs the request and returns the status code and body, whatever
// the status.
func (c *Client) send(ctx context.Context, method, path string, in any) (int, []byte, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return 0, nil, fmt.Errorf("lettermint: encode request: %w", err)
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("lettermint: %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("lettermint: read response: %w", err)
	}
	return resp.StatusCode, respBody, nil
}

func newAPIError(status int, body []byte) *APIError {
	apiErr := &APIError{StatusCode: status}
	var payload struct {
		Message string              `json:"message"`
		Errors  map[string][]string `json:"errors"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &payload) == nil {
		apiErr.Message = payload.Message
		if apiErr.Message == "" {
			apiErr.Message = payload.Error.Message
		}
		apiErr.Errors = payload.Errors
	}
	return apiErr
}
