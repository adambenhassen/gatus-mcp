package main

import (
	"context"
	"testing"
	"time"
)

func TestGetenv(t *testing.T) {
	t.Setenv("GATUS_MCP_TEST_VAR", "set")
	if got := getenv("GATUS_MCP_TEST_VAR", "fallback"); got != "set" {
		t.Errorf("getenv with value set = %q, want set", got)
	}
	if got := getenv("GATUS_MCP_MISSING_VAR", "fallback"); got != "fallback" {
		t.Errorf("getenv missing = %q, want fallback", got)
	}
}

func TestRunGracefulShutdown(t *testing.T) {
	t.Setenv("MCP_PORT", "0") // bind an ephemeral port
	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan error, 1)
	go func() { done <- run(ctx) }()

	time.Sleep(100 * time.Millisecond) // let the listener bind
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned %v, want nil on graceful shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not shut down within 5s")
	}
}

func TestRunBindError(t *testing.T) {
	t.Setenv("MCP_PORT", "-1") // invalid port -> ListenAndServe fails
	if err := run(t.Context()); err == nil {
		t.Fatal("expected a bind error for an invalid port")
	}
}
