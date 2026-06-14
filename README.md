# gatus-mcp

[![CI](https://github.com/adambenhassen/gatus-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/adambenhassen/gatus-mcp/actions/workflows/ci.yml)

An [MCP](https://modelcontextprotocol.io) server that exposes a
[Gatus](https://github.com/TwiN/gatus) status page to AI agents. It wraps Gatus's
REST API and serves tools over streamable HTTP at `/mcp`, so an assistant can
answer "what's up / what's down" and drill into uptime, response times, and probe
history.

Monitors are defined in Gatus's own YAML config — this server does **not** create
or change them. Every tool is read-only except `submit_external_result`, which
pushes results for endpoints Gatus is configured to treat as external.

## Tools

| Tool | Description |
| --- | --- |
| `get_health_summary` | Counts of healthy/down/pending endpoints + the list of those currently down. Start here. |
| `list_endpoint_statuses` | All endpoints with current status; optional `only_unhealthy` and `group` filters. |
| `get_endpoint_history` | Recent probe history for one endpoint (paginated). |
| `get_endpoint_uptime` | Uptime ratio over a window. |
| `get_endpoint_response_time` | Average response time (ms) over a window. |
| `get_response_time_history` | Response-time history series over a window. |
| `list_suite_statuses` / `get_suite_status` | Gatus suite statuses. |
| `get_config` | Gatus instance configuration metadata. |
| `get_liveness` | Whether the Gatus instance itself is up. |
| `get_health_badge` / `get_health_badge_shields` | Health badge as SVG / shields.io JSON. |
| `get_uptime_badge` / `get_response_time_badge` / `get_response_time_chart` | Badge & chart SVGs. |
| `submit_external_result` | Submit a result for an `external` endpoint (write; needs `GATUS_TOKEN`). |

Windowed tools accept a `duration` of `1h`, `24h`, `7d`, or `30d`.

## Configuration

All configuration is via environment variables:

| Variable | Default | Purpose |
| --- | --- | --- |
| `GATUS_URL` | `http://gatus:8080` | Base URL of the Gatus instance. |
| `MCP_HOST` | `0.0.0.0` | Listen host. |
| `MCP_PORT` | `3000` | Listen port. |
| `GATUS_TOKEN` | _(unset)_ | Bearer token; only needed by `submit_external_result`. |

## Running

### Docker

```sh
docker build -t gatus-mcp .
docker run --rm -p 3000:3000 -e GATUS_URL=http://your-gatus:8080 gatus-mcp
```

### From source

```sh
go run ./cmd/gatus-mcp
# or
go build -o gatus-mcp ./cmd/gatus-mcp && GATUS_URL=http://localhost:8080 ./gatus-mcp
```

The MCP endpoint is then available at `http://localhost:3000/mcp`.

## Connecting a client

Point any streamable-HTTP MCP client at `http://<host>:3000/mcp`. For example,
with Claude Code:

```sh
claude mcp add --transport http gatus http://localhost:3000/mcp
```

## Development

```sh
go test ./...        # unit + parity tests
go vet ./...
golangci-lint run    # config in .golangci.yml
```

### Layout

```
cmd/gatus-mcp/      entrypoint: env config + HTTP serving
internal/gatus/     REST client for the Gatus API
internal/mcpserver/ MCP tool definitions and handlers
```

## License

[MIT](LICENSE) © Adam Benhassen
