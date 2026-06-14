package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/adambenhassen/gatus-mcp/internal/gatus"
)

// emptyInput is the input type for tools that take no arguments.
type emptyInput struct{}

// allowedDurations is the single source of truth for the duration windows Gatus
// accepts on the uptime, response-time, badge and chart endpoints.
var allowedDurations = []string{"1h", "24h", "7d", "30d"}

// trimmedEndpoint is the reduced view of a Gatus endpoint returned by the summary
// and list tools. healthy is always serialized — null when no probe has run yet
// (the "pending" state), so consumers can tell pending apart from up/down. The
// remaining optional fields are co-set with healthy by trim and omitted when there
// is no result.
type trimmedEndpoint struct {
	Healthy          *bool    `json:"healthy"`
	Status           *int     `json:"status,omitempty"`
	ResponseTimeMs   *float64 `json:"responseTimeMs,omitempty"`
	Name             string   `json:"name"`
	Group            string   `json:"group"`
	Key              string   `json:"key"`
	Timestamp        string   `json:"timestamp,omitempty"`
	Note             string   `json:"note,omitempty"`
	FailedConditions []string `json:"failedConditions,omitempty"`
	Errors           []string `json:"errors,omitempty"`
}

// roundMs converts a nanosecond duration to milliseconds rounded to one decimal.
func roundMs(durationNS int64) float64 {
	return math.Round(float64(durationNS)/1e6*10) / 10
}

// trim reduces a Gatus endpoint to the essentials, keyed off its latest result.
func trim(e gatus.Endpoint) trimmedEndpoint {
	out := trimmedEndpoint{
		Name:  e.Name,
		Group: e.Group,
		Key:   e.Key,
	}
	if len(e.Results) == 0 {
		out.Note = "no probe results yet"
		return out
	}
	last := e.Results[len(e.Results)-1]
	healthy := last.Success
	out.Healthy = &healthy
	status := last.Status
	out.Status = &status
	ms := roundMs(last.Duration)
	out.ResponseTimeMs = &ms
	out.Timestamp = last.Timestamp

	failed := make([]string, 0, len(last.ConditionResults))
	for _, cr := range last.ConditionResults {
		if !cr.Success {
			failed = append(failed, cr.Condition)
		}
	}
	out.FailedConditions = failed
	if len(last.Errors) > 0 {
		out.Errors = last.Errors
	}
	return out
}

// requireKey validates that an endpoint key was provided.
func requireKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return errors.New("key is required")
	}
	return nil
}

// validateDuration validates a duration window against the set Gatus accepts.
func validateDuration(d string) error {
	if !slices.Contains(allowedDurations, d) {
		return fmt.Errorf("invalid duration %q: must be one of %s", d, strings.Join(allowedDurations, ", "))
	}
	return nil
}

// endpointPath builds /api/v1/endpoints/{key}/<segments...> with the key escaped,
// so a caller-supplied key cannot inject path, query, or fragment characters.
func endpointPath(key string, segments ...string) string {
	base := "/api/v1/endpoints/" + url.PathEscape(key)
	if len(segments) == 0 {
		return base
	}
	return base + "/" + strings.Join(segments, "/")
}

// --- Core endpoint status tools ---

// summaryOutput is the result of get_health_summary.
type summaryOutput struct {
	Total     int               `json:"total"`
	Up        int               `json:"up"`
	Down      int               `json:"down"`
	Pending   int               `json:"pending"`
	Unhealthy []trimmedEndpoint `json:"unhealthy"`
}

func (t *toolset) healthSummary(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, summaryOutput, error) {
	endpoints, err := t.client.FetchStatuses(ctx)
	if err != nil {
		return nil, summaryOutput{}, err
	}
	out := summaryOutput{
		Total:     len(endpoints),
		Unhealthy: make([]trimmedEndpoint, 0, len(endpoints)),
	}
	for _, e := range endpoints {
		te := trim(e)
		switch {
		case te.Healthy == nil:
			out.Pending++
		case *te.Healthy:
			out.Up++
		default:
			out.Down++
			out.Unhealthy = append(out.Unhealthy, te)
		}
	}
	return nil, out, nil
}

// listInput is the input for list_endpoint_statuses.
type listInput struct {
	OnlyUnhealthy bool   `json:"only_unhealthy,omitempty" jsonschema:"only return endpoints that are currently down"`
	Group         string `json:"group,omitempty"          jsonschema:"filter to a single group, case-insensitive (e.g. media, ai, infra)"`
}

// listOutput wraps the endpoint slice in an object: MCP structured tool output
// must be a JSON object, not a top-level array.
type listOutput struct {
	Result []trimmedEndpoint `json:"result"`
}

func (t *toolset) listStatuses(ctx context.Context, _ *mcp.CallToolRequest, in listInput) (*mcp.CallToolResult, listOutput, error) {
	endpoints, err := t.client.FetchStatuses(ctx)
	if err != nil {
		return nil, listOutput{}, err
	}
	group := strings.ToLower(in.Group)
	result := make([]trimmedEndpoint, 0, len(endpoints))
	for _, e := range endpoints {
		te := trim(e)
		if group != "" && strings.ToLower(te.Group) != group {
			continue
		}
		if in.OnlyUnhealthy && (te.Healthy == nil || *te.Healthy) {
			continue
		}
		result = append(result, te)
	}
	return nil, listOutput{Result: result}, nil
}

// historyInput is the input for get_endpoint_history.
type historyInput struct {
	Key      string `json:"key"                 jsonschema:"the Gatus key from list_endpoint_statuses, e.g. infra_planka"`
	Page     int    `json:"page,omitempty"      jsonschema:"1-based page number (default 1)"`
	PageSize int    `json:"page_size,omitempty" jsonschema:"results per page (default 20)"`
}

// historyResult is a single probe result in get_endpoint_history.
type historyResult struct {
	Status         int      `json:"status"`
	Success        bool     `json:"success"`
	ResponseTimeMs float64  `json:"responseTimeMs"`
	Timestamp      string   `json:"timestamp"`
	Errors         []string `json:"errors"`
}

// historyOutput is the result of get_endpoint_history.
type historyOutput struct {
	Name    string          `json:"name"`
	Group   string          `json:"group"`
	Key     string          `json:"key"`
	Results []historyResult `json:"results"`
}

func (t *toolset) endpointHistory(ctx context.Context, _ *mcp.CallToolRequest, in historyInput) (*mcp.CallToolResult, historyOutput, error) {
	if err := requireKey(in.Key); err != nil {
		return nil, historyOutput{}, err
	}
	page := in.Page
	if page == 0 {
		page = 1
	}
	pageSize := in.PageSize
	if pageSize == 0 {
		pageSize = 20
	}
	q := url.Values{}
	q.Set("page", strconv.Itoa(page))
	q.Set("pageSize", strconv.Itoa(pageSize))

	var ep gatus.Endpoint
	if err := t.client.GetJSON(ctx, endpointPath(in.Key, "statuses"), q, &ep); err != nil {
		return nil, historyOutput{}, err
	}
	results := make([]historyResult, 0, len(ep.Results))
	for _, r := range ep.Results {
		errs := r.Errors
		if errs == nil {
			errs = []string{}
		}
		results = append(results, historyResult{
			Status:         r.Status,
			Success:        r.Success,
			ResponseTimeMs: roundMs(r.Duration),
			Timestamp:      r.Timestamp,
			Errors:         errs,
		})
	}
	key := ep.Key
	if key == "" {
		key = in.Key
	}
	return nil, historyOutput{Name: ep.Name, Group: ep.Group, Key: key, Results: results}, nil
}

// --- New tools: full Gatus API coverage ---

// metricInput is shared by the duration-windowed tools: raw uptime/response-time,
// response-time history, and the badge/chart tools.
type metricInput struct {
	Key      string `json:"key"      jsonschema:"the Gatus endpoint key, e.g. infra_planka"`
	Duration string `json:"duration" jsonschema:"window: one of 1h, 24h, 7d, 30d"`
}

// rawMetricOutput is the result of the raw uptime/response-time tools.
type rawMetricOutput struct {
	Key      string  `json:"key"`
	Duration string  `json:"duration"`
	Value    float64 `json:"value"`
}

func (t *toolset) rawMetric(ctx context.Context, in metricInput, kind string) (rawMetricOutput, error) {
	if err := requireKey(in.Key); err != nil {
		return rawMetricOutput{}, err
	}
	if err := validateDuration(in.Duration); err != nil {
		return rawMetricOutput{}, err
	}
	raw, err := t.client.GetText(ctx, endpointPath(in.Key, kind, in.Duration))
	if err != nil {
		return rawMetricOutput{}, err
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return rawMetricOutput{}, fmt.Errorf("gatus returned a non-numeric %s value %q: %w", kind, raw, err)
	}
	return rawMetricOutput{Key: in.Key, Duration: in.Duration, Value: value}, nil
}

func (t *toolset) endpointUptime(ctx context.Context, _ *mcp.CallToolRequest, in metricInput) (*mcp.CallToolResult, rawMetricOutput, error) {
	out, err := t.rawMetric(ctx, in, "uptimes")
	if err != nil {
		return nil, rawMetricOutput{}, err
	}
	return nil, out, nil
}

func (t *toolset) endpointResponseTime(ctx context.Context, _ *mcp.CallToolRequest, in metricInput) (*mcp.CallToolResult, rawMetricOutput, error) {
	out, err := t.rawMetric(ctx, in, "response-times")
	if err != nil {
		return nil, rawMetricOutput{}, err
	}
	return nil, out, nil
}

func (t *toolset) responseTimeHistory(ctx context.Context, _ *mcp.CallToolRequest, in metricInput) (*mcp.CallToolResult, any, error) {
	if err := requireKey(in.Key); err != nil {
		return nil, nil, err
	}
	if err := validateDuration(in.Duration); err != nil {
		return nil, nil, err
	}
	out, err := t.passthrough(ctx, endpointPath(in.Key, "response-times", in.Duration, "history"))
	if err != nil {
		return nil, nil, err
	}
	return nil, out, nil
}

// svgInput is the input for badge/chart tools that take only a key.
type svgInput struct {
	Key string `json:"key" jsonschema:"the Gatus endpoint key, e.g. infra_planka"`
}

// svgOutput carries raw SVG markup returned by Gatus badge and chart endpoints.
type svgOutput struct {
	Key      string `json:"key"`
	Duration string `json:"duration,omitempty"`
	SVG      string `json:"svg"`
}

func (t *toolset) svg(ctx context.Context, key, duration, path string) (svgOutput, error) {
	if err := requireKey(key); err != nil {
		return svgOutput{}, err
	}
	if duration != "" {
		if err := validateDuration(duration); err != nil {
			return svgOutput{}, err
		}
	}
	body, err := t.client.GetText(ctx, path)
	if err != nil {
		return svgOutput{}, err
	}
	return svgOutput{Key: key, Duration: duration, SVG: body}, nil
}

func (t *toolset) healthBadge(ctx context.Context, _ *mcp.CallToolRequest, in svgInput) (*mcp.CallToolResult, svgOutput, error) {
	out, err := t.svg(ctx, in.Key, "", endpointPath(in.Key, "health", "badge.svg"))
	if err != nil {
		return nil, svgOutput{}, err
	}
	return nil, out, nil
}

func (t *toolset) uptimeBadge(ctx context.Context, _ *mcp.CallToolRequest, in metricInput) (*mcp.CallToolResult, svgOutput, error) {
	out, err := t.svg(ctx, in.Key, in.Duration, endpointPath(in.Key, "uptimes", in.Duration, "badge.svg"))
	if err != nil {
		return nil, svgOutput{}, err
	}
	return nil, out, nil
}

func (t *toolset) responseTimeBadge(ctx context.Context, _ *mcp.CallToolRequest, in metricInput) (*mcp.CallToolResult, svgOutput, error) {
	out, err := t.svg(ctx, in.Key, in.Duration, endpointPath(in.Key, "response-times", in.Duration, "badge.svg"))
	if err != nil {
		return nil, svgOutput{}, err
	}
	return nil, out, nil
}

func (t *toolset) responseTimeChart(ctx context.Context, _ *mcp.CallToolRequest, in metricInput) (*mcp.CallToolResult, svgOutput, error) {
	out, err := t.svg(ctx, in.Key, in.Duration, endpointPath(in.Key, "response-times", in.Duration, "chart.svg"))
	if err != nil {
		return nil, svgOutput{}, err
	}
	return nil, out, nil
}

func (t *toolset) healthBadgeShields(ctx context.Context, _ *mcp.CallToolRequest, in svgInput) (*mcp.CallToolResult, any, error) {
	if err := requireKey(in.Key); err != nil {
		return nil, nil, err
	}
	out, err := t.passthrough(ctx, endpointPath(in.Key, "health", "badge.shields"))
	if err != nil {
		return nil, nil, err
	}
	return nil, out, nil
}

// suiteInput is the input for get_suite_status.
type suiteInput struct {
	Key string `json:"key" jsonschema:"the Gatus suite key"`
}

func (t *toolset) suiteStatuses(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, any, error) {
	out, err := t.passthrough(ctx, "/api/v1/suites/statuses")
	if err != nil {
		return nil, nil, err
	}
	return nil, out, nil
}

func (t *toolset) suiteStatus(ctx context.Context, _ *mcp.CallToolRequest, in suiteInput) (*mcp.CallToolResult, any, error) {
	if err := requireKey(in.Key); err != nil {
		return nil, nil, err
	}
	out, err := t.passthrough(ctx, "/api/v1/suites/"+url.PathEscape(in.Key)+"/statuses")
	if err != nil {
		return nil, nil, err
	}
	return nil, out, nil
}

func (t *toolset) config(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, any, error) {
	out, err := t.passthrough(ctx, "/api/v1/config")
	if err != nil {
		return nil, nil, err
	}
	return nil, out, nil
}

func (t *toolset) liveness(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, any, error) {
	out, err := t.passthrough(ctx, "/health")
	if err != nil {
		return nil, nil, err
	}
	return nil, out, nil
}

// externalInput is the input for submit_external_result (a write).
type externalInput struct {
	Key      string `json:"key"                jsonschema:"the Gatus key of an endpoint configured as external"`
	Success  bool   `json:"success"            jsonschema:"whether the external check passed"`
	Error    string `json:"error,omitempty"    jsonschema:"optional error message to record when success is false"`
	Duration string `json:"duration,omitempty" jsonschema:"optional probe duration, e.g. 250ms"`
}

// externalOutput is the result of submit_external_result.
type externalOutput struct {
	Response string `json:"response"`
}

func (t *toolset) submitExternal(ctx context.Context, _ *mcp.CallToolRequest, in externalInput) (*mcp.CallToolResult, externalOutput, error) {
	if err := requireKey(in.Key); err != nil {
		return nil, externalOutput{}, err
	}
	if !t.client.HasToken() {
		return nil, externalOutput{}, errors.New("GATUS_TOKEN must be set to submit external results")
	}
	resp, err := t.client.PostExternal(ctx, in.Key, in.Success, in.Error, in.Duration)
	if err != nil {
		return nil, externalOutput{}, err
	}
	return nil, externalOutput{Response: resp}, nil
}

// passthrough fetches a Gatus JSON endpoint and returns the decoded payload
// unaltered, for endpoints where a typed struct adds little value (dynamic, large,
// or trivially small fixed shapes).
func (t *toolset) passthrough(ctx context.Context, path string) (any, error) {
	var out any
	if err := t.client.GetJSON(ctx, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
