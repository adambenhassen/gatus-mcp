package gatus

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestNewClientTrimsTrailingSlash(t *testing.T) {
	c := NewClient("http://gatus:8080/", "")
	if c.baseURL != "http://gatus:8080" {
		t.Errorf("baseURL = %q, want trailing slash trimmed", c.baseURL)
	}
}

func TestHasToken(t *testing.T) {
	if NewClient("http://x", "").HasToken() {
		t.Error("HasToken = true for empty token")
	}
	if !NewClient("http://x", "tok").HasToken() {
		t.Error("HasToken = false for set token")
	}
}

func TestFetchStatuses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/endpoints/statuses" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if _, err := io.WriteString(w, `[{"name":"A","key":"a","results":[{"status":200,"success":true,"duration":1000000}]}]`); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	eps, err := NewClient(srv.URL, "").FetchStatuses(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 1 || eps[0].Key != "a" || !eps[0].Results[0].Success {
		t.Errorf("unexpected endpoints: %+v", eps)
	}
}

func TestGetTextNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	_, err := NewClient(srv.URL, "").GetText(t.Context(), "/x")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestGetJSONInvalid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := io.WriteString(w, `{not json`); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	var out any
	if err := NewClient(srv.URL, "").GetJSON(t.Context(), "/x", nil, &out); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestConnectionError(t *testing.T) {
	srv := httptest.NewServer(http.NewServeMux())
	srv.Close() // close immediately so the address refuses connections

	if _, err := NewClient(srv.URL, "").FetchStatuses(t.Context()); err == nil {
		t.Fatal("expected connection error")
	}
}

func TestPostExternal(t *testing.T) {
	var (
		gotQuery  url.Values
		gotAuth   string
		gotMethod string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		gotAuth = r.Header.Get("Authorization")
		gotMethod = r.Method
		if _, err := io.WriteString(w, "ok"); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	resp, err := NewClient(srv.URL, "secret").PostExternal(t.Context(), "key1", false, "boom", "250ms")
	if err != nil {
		t.Fatal(err)
	}
	if resp != "ok" {
		t.Errorf("resp = %q, want ok", resp)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotAuth != "Bearer secret" {
		t.Errorf("auth = %q, want Bearer secret", gotAuth)
	}
	if gotQuery.Get("success") != "false" || gotQuery.Get("error") != "boom" || gotQuery.Get("duration") != "250ms" {
		t.Errorf("unexpected query: %v", gotQuery)
	}
}

func TestPostExternalOmitsEmptyParams(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	if _, err := NewClient(srv.URL, "tok").PostExternal(t.Context(), "k", true, "", ""); err != nil {
		t.Fatal(err)
	}
	if gotQuery.Has("error") || gotQuery.Has("duration") {
		t.Errorf("optional params should be omitted, got %v", gotQuery)
	}
	if gotQuery.Get("success") != "true" {
		t.Errorf("success = %q, want true", gotQuery.Get("success"))
	}
}
