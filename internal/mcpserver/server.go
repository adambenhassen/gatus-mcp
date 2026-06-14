// Package mcpserver exposes a Gatus instance as a set of MCP tools.
package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/adambenhassen/gatus-mcp/internal/gatus"
)

// toolset binds the MCP tool handlers to a Gatus client.
type toolset struct {
	client *gatus.Client
}

// New builds an MCP server with every Gatus tool registered. version is reported
// to MCP clients in serverInfo.
func New(client *gatus.Client, version string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "gatus", Version: version}, nil)
	(&toolset{client: client}).register(server)
	return server
}

// register adds every Gatus tool to the MCP server.
func (t *toolset) register(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "get_health_summary",
		Description: "START HERE for status overview questions. Returns a count of healthy vs " +
			"unhealthy monitored endpoints and the list of any that are currently down " +
			"(with the failed conditions / errors).",
	}, t.healthSummary)
	mcp.AddTool(s, &mcp.Tool{
		Name: "list_endpoint_statuses",
		Description: "List monitored endpoints with their current status (name, group, healthy, " +
			"HTTP status, response time, failed conditions). Optionally filter to only " +
			"unhealthy endpoints and/or a single group (e.g. 'media', 'ai', 'infra').",
	}, t.listStatuses)
	mcp.AddTool(s, &mcp.Tool{
		Name: "get_endpoint_history",
		Description: "Get recent probe history for a single endpoint by its Gatus key " +
			"(the 'key' field from list_endpoint_statuses, e.g. 'infra_planka'). Returns " +
			"the recent results: status, success, response time, timestamp, errors.",
	}, t.endpointHistory)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_endpoint_uptime",
		Description: "Get the uptime ratio for an endpoint over a window (1h, 24h, 7d, 30d).",
	}, t.endpointUptime)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_endpoint_response_time",
		Description: "Get the average response time (ms) for an endpoint over a window (1h, 24h, 7d, 30d).",
	}, t.endpointResponseTime)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_response_time_history",
		Description: "Get the response-time history series for an endpoint over a window (1h, 24h, 7d, 30d).",
	}, t.responseTimeHistory)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_suite_statuses",
		Description: "List all Gatus suites with their current status.",
	}, t.suiteStatuses)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_suite_status",
		Description: "Get the current status of a single Gatus suite by its key.",
	}, t.suiteStatus)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_config",
		Description: "Get the Gatus instance configuration metadata (UI settings, OIDC, etc.).",
	}, t.config)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_liveness",
		Description: "Liveness probe: returns whether the Gatus instance itself is up.",
	}, t.liveness)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_health_badge",
		Description: "Get the health badge for an endpoint as raw SVG markup.",
	}, t.healthBadge)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_health_badge_shields",
		Description: "Get the health badge for an endpoint in shields.io JSON endpoint format.",
	}, t.healthBadgeShields)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_uptime_badge",
		Description: "Get the uptime badge for an endpoint over a window (1h, 24h, 7d, 30d) as raw SVG markup.",
	}, t.uptimeBadge)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_response_time_badge",
		Description: "Get the response-time badge for an endpoint over a window (1h, 24h, 7d, 30d) as raw SVG markup.",
	}, t.responseTimeBadge)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_response_time_chart",
		Description: "Get the response-time chart for an endpoint over a window (1h, 24h, 7d, 30d) as raw SVG markup.",
	}, t.responseTimeChart)
	mcp.AddTool(s, &mcp.Tool{
		Name: "submit_external_result",
		Description: "Submit a result for an endpoint configured as 'external' in Gatus. Requires " +
			"GATUS_TOKEN to be set. This is the only tool that mutates Gatus state.",
	}, t.submitExternal)
}
