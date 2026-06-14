//go:build e2e

// Package mcpserver e2e test: drives the full MCP client -> server -> live Gatus
// stack. Requires a running Gatus reachable at $GATUS_URL. Run with:
//
//	GATUS_URL=http://localhost:8080 go test -tags=e2e ./internal/mcpserver/
package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/adambenhassen/gatus-mcp/internal/gatus"
)

func TestE2ELiveGatus(t *testing.T) {
	gatusURL := os.Getenv("GATUS_URL")
	if gatusURL == "" {
		t.Skip("GATUS_URL not set; skipping live e2e")
	}

	server := New(gatus.NewClient(gatusURL, ""), "e2e")
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	httpSrv := httptest.NewServer(handler)
	t.Cleanup(httpSrv.Close)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "gatus-mcp-e2e", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: httpSrv.URL}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		if cerr := session.Close(); cerr != nil {
			t.Errorf("close session: %v", cerr)
		}
	})

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools.Tools) != 16 {
		t.Errorf("got %d tools, want 16", len(tools.Tools))
	}

	summary := callStruct[summaryOutput](ctx, t, session, "get_health_summary", map[string]any{})
	if summary.Total < 1 {
		t.Errorf("expected at least one monitored endpoint, got total=%d", summary.Total)
	}
	if summary.Up+summary.Down+summary.Pending != summary.Total {
		t.Errorf("counts do not sum: up=%d down=%d pending=%d total=%d",
			summary.Up, summary.Down, summary.Pending, summary.Total)
	}

	list := callStruct[listOutput](ctx, t, session, "list_endpoint_statuses", map[string]any{})
	if len(list.Result) != summary.Total {
		t.Errorf("list returned %d endpoints, summary reported %d", len(list.Result), summary.Total)
	}

	// liveness returns a dynamic object; just assert the call succeeds and is non-error.
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_liveness", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("call get_liveness: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_liveness returned a tool error: %v", res.Content)
	}
}

// callStruct calls a tool and decodes its structured output into T.
func callStruct[T any](ctx context.Context, t *testing.T, session *mcp.ClientSession, name string, args map[string]any) T {
	t.Helper()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s returned a tool error: %v", name, res.Content)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal %s result: %v", name, err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s result: %v", name, err)
	}
	return out
}
