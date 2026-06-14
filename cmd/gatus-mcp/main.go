// Command gatus-mcp exposes a Gatus status page over MCP (streamable HTTP).
//
// It serves MCP tools at /mcp. Monitors are defined in Gatus's own YAML config;
// the read tools answer "what's up / down", and the single write tool
// (submit_external_result) pushes results for endpoints Gatus is configured to
// treat as external.
//
// Configuration (environment):
//
//	GATUS_URL    base URL of the Gatus instance (default http://gatus:8080)
//	MCP_HOST     listen host (default 0.0.0.0)
//	MCP_PORT     listen port (default 3000)
//	GATUS_TOKEN  bearer token, only needed by submit_external_result (optional)
package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/adambenhassen/gatus-mcp/internal/gatus"
	"github.com/adambenhassen/gatus-mcp/internal/mcpserver"
)

// shutdownTimeout bounds how long in-flight requests have to drain on shutdown.
const shutdownTimeout = 10 * time.Second

// version is the build version reported to MCP clients. It is set at release
// time via -ldflags "-X main.version=<tag>"; defaults to "dev" otherwise.
var version = "dev"

// getenv returns the environment value for key, or def when it is unset/empty.
func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

// run starts the MCP HTTP server and blocks until ctx is cancelled or an interrupt
// signal arrives, then shuts down gracefully. It returns the first fatal error.
func run(ctx context.Context) error {
	gatusURL := getenv("GATUS_URL", "http://gatus:8080")
	host := getenv("MCP_HOST", "0.0.0.0")
	port := getenv("MCP_PORT", "3000")
	token := os.Getenv("GATUS_TOKEN")

	server := mcpserver.New(gatus.NewClient(gatusURL, token), version)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	mux.Handle("/mcp/", handler)

	addr := net.JoinHostPort(host, port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		log.Printf("gatus-mcp listening on %s/mcp (gatus: %s)", addr, gatusURL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
