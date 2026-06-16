package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_Get(t *testing.T) {
	expected := map[string]string{"org_id": "org-123", "name": "Test"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected auth header, got %s", r.Header.Get("Authorization"))
		}
		if err := json.NewEncoder(w).Encode(expected); err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, Token: "test-token", HTTPClient: server.Client()}

	var result map[string]string
	err := client.Get(context.Background(), "/test", &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["org_id"] != "org-123" {
		t.Errorf("expected org-123, got %s", result["org_id"])
	}
}

func TestClient_TrailingSlashBaseURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/test" {
			t.Errorf("expected path /v3/test, got %s", r.URL.Path)
		}
		if err := json.NewEncoder(w).Encode(map[string]string{"ok": "true"}); err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL + "/", Token: "test-token", HTTPClient: server.Client()}

	if err := client.Get(context.Background(), "/v3/test", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_Post(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected json content type, got %s", r.Header.Get("Content-Type"))
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decoding request: %v", err)
		}
		resp := map[string]string{"org_id": "org-new", "name": body["name"]}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, Token: "test-token", HTTPClient: server.Client()}

	var result map[string]string
	err := client.Post(context.Background(), "/orgs", map[string]string{"name": "New"}, &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["name"] != "New" {
		t.Errorf("expected New, got %s", result["name"])
	}
}

func TestClient_ErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		if err := json.NewEncoder(w).Encode(map[string]string{"detail": "Not found"}); err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, Token: "test-token", HTTPClient: server.Client()}

	err := client.Get(context.Background(), "/missing", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !IsNotFound(err) {
		t.Errorf("expected not found error, got %v", err)
	}
	apiErr := err.(*APIError)
	if apiErr.Detail != "Not found" {
		t.Errorf("expected 'Not found', got %s", apiErr.Detail)
	}
}

func TestClient_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		if _, err := w.Write([]byte("internal server error")); err != nil {
			t.Errorf("writing response: %v", err)
		}
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, Token: "test-token", HTTPClient: server.Client()}

	err := client.Get(context.Background(), "/broken", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	apiErr := err.(*APIError)
	if apiErr.StatusCode != 500 {
		t.Errorf("expected 500, got %d", apiErr.StatusCode)
	}
}

func TestClient_RetriesTransientErrors(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		switch attempts {
		case 1:
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			w.WriteHeader(http.StatusBadGateway)
		default:
			if err := json.NewEncoder(w).Encode(map[string]string{"ok": "true"}); err != nil {
				t.Errorf("encoding response: %v", err)
			}
		}
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, Token: "test-token", HTTPClient: NewRetryHTTPClient()}

	if err := client.Get(context.Background(), "/test", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestClient_PostRetriesOnlyRateLimits(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]string{"ok": "true"}); err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, Token: "test-token", HTTPClient: NewRetryHTTPClient(), MutatingHTTPClient: NewMutatingRetryHTTPClient()}

	if err := client.Post(context.Background(), "/test", map[string]string{"name": "x"}, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestClient_PostDoesNotRetryServerErrors(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, Token: "test-token", HTTPClient: NewRetryHTTPClient(), MutatingHTTPClient: NewMutatingRetryHTTPClient()}

	err := client.Post(context.Background(), "/test", map[string]string{"name": "x"}, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502 APIError, got %v", err)
	}
	if attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", attempts)
	}
}

func TestClient_PatchDoesNotRetryServerErrors(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, Token: "test-token", HTTPClient: NewRetryHTTPClient(), MutatingHTTPClient: NewMutatingRetryHTTPClient()}

	err := client.Patch(context.Background(), "/test", map[string]string{"name": "x"}, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502 APIError, got %v", err)
	}
	if attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", attempts)
	}
}
