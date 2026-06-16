package provider

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

	"github.com/hashicorp/go-retryablehttp"
)

type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
	// MutatingHTTPClient, when set, is used for non-idempotent requests
	// (POST and PATCH) instead of HTTPClient. These methods may create or
	// mutate resources with side effects, so they get a more conservative
	// retry policy.
	MutatingHTTPClient *http.Client
}

// NewRetryHTTPClient returns an HTTP client that retries transient failures
// (connection errors, 429s, and 5xxs) with exponential backoff. Only use it
// for idempotent requests: a retried request may be executed twice. The
// per-attempt timeout applies to each try.
func NewRetryHTTPClient() *http.Client {
	retryClient := newRetryClient()
	return retryClient.StandardClient()
}

// NewMutatingRetryHTTPClient returns an HTTP client for non-idempotent requests
// (POST, PATCH). It retries only 429 responses — a rate-limited request was
// never processed, so resending cannot create duplicates, while connection
// errors and 5xxs may have committed server-side and are surfaced to the caller.
func NewMutatingRetryHTTPClient() *http.Client {
	retryClient := newRetryClient()
	retryClient.CheckRetry = func(ctx context.Context, resp *http.Response, err error) (bool, error) {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return err == nil && resp != nil && resp.StatusCode == http.StatusTooManyRequests, nil
	}
	return retryClient.StandardClient()
}

func newRetryClient() *retryablehttp.Client {
	retryClient := retryablehttp.NewClient()
	retryClient.RetryMax = 4
	retryClient.Logger = nil
	retryClient.HTTPClient.Timeout = 60 * time.Second
	return retryClient
}

type APIError struct {
	StatusCode int
	Detail     string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error %d: %s", e.StatusCode, e.Detail)
}

func (c *Client) do(ctx context.Context, method, path string, body any, result any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshaling request: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, reqBody)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// POST/PATCH use MutatingHTTPClient (429-only retries) because they are
	// not idempotent — a retried create can produce duplicates. GET/PUT/DELETE
	// are idempotent and safe to retry on transient errors.
	httpClient := c.HTTPClient
	if (method == http.MethodPost || method == http.MethodPatch) && c.MutatingHTTPClient != nil {
		httpClient = c.MutatingHTTPClient
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	// A Close error after the body has been fully read is not actionable.
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		detail := string(respBody)
		var parsed struct {
			Detail string `json:"detail"`
		}
		if json.Unmarshal(respBody, &parsed) == nil && parsed.Detail != "" {
			detail = parsed.Detail
		}
		return &APIError{StatusCode: resp.StatusCode, Detail: detail}
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("unmarshaling response: %w", err)
		}
	}

	return nil
}

func (c *Client) Get(ctx context.Context, path string, result any) error {
	return c.do(ctx, http.MethodGet, path, nil, result)
}

func (c *Client) Post(ctx context.Context, path string, body any, result any) error {
	return c.do(ctx, http.MethodPost, path, body, result)
}

func (c *Client) Patch(ctx context.Context, path string, body any, result any) error {
	return c.do(ctx, http.MethodPatch, path, body, result)
}

func (c *Client) Put(ctx context.Context, path string, body any, result any) error {
	return c.do(ctx, http.MethodPut, path, body, result)
}

func (c *Client) Delete(ctx context.Context, path string, result any) error {
	return c.do(ctx, http.MethodDelete, path, nil, result)
}

func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == 404
}
