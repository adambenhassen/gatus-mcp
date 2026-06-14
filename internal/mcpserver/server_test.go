package mcpserver

import (
	"testing"

	"github.com/adambenhassen/gatus-mcp/internal/gatus"
)

func TestNew(t *testing.T) {
	if New(gatus.NewClient("http://gatus:8080", "")) == nil {
		t.Fatal("New returned nil server")
	}
}
