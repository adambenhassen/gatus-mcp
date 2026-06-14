package main

import "testing"

func TestGetenv(t *testing.T) {
	t.Setenv("GATUS_MCP_TEST_VAR", "set")
	if got := getenv("GATUS_MCP_TEST_VAR", "fallback"); got != "set" {
		t.Errorf("getenv with value set = %q, want set", got)
	}
	if got := getenv("GATUS_MCP_MISSING_VAR", "fallback"); got != "fallback" {
		t.Errorf("getenv missing = %q, want fallback", got)
	}
}
