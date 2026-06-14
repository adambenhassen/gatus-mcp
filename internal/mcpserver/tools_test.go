package mcpserver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/adambenhassen/gatus-mcp/internal/gatus"
)

const statusesJSON = `[
  {"name":"Planka","group":"infra","key":"infra_planka","results":[
    {"status":200,"success":true,"duration":12340000,"timestamp":"2026-06-14T12:00:00Z","conditionResults":[{"condition":"[STATUS] == 200","success":true}],"errors":[]}
  ]},
  {"name":"Ollama","group":"ai","key":"ai_ollama","results":[
    {"status":500,"success":false,"duration":250000000,"timestamp":"2026-06-14T12:00:00Z","conditionResults":[{"condition":"[STATUS] == 200","success":false},{"condition":"[CONNECTED] == true","success":true}],"errors":["connection refused"]}
  ]},
  {"name":"New","group":"","key":"new","results":[]}
]`

const historyJSON = `{"name":"Planka","group":"infra","key":"infra_planka","results":[
  {"status":200,"success":true,"duration":12340000,"timestamp":"2026-06-14T12:00:00Z","conditionResults":[],"errors":[]},
  {"status":500,"success":false,"duration":99000000,"timestamp":"2026-06-14T12:00:30Z","conditionResults":[],"errors":["boom"]}
]}`

// Trimmed views reused across the expected outputs below.
const (
	healthyJSON = `{"name":"Planka","group":"infra","key":"infra_planka","healthy":true,"status":200,"responseTimeMs":12.3,"timestamp":"2026-06-14T12:00:00Z"}`
	downJSON    = `{"name":"Ollama","group":"ai","key":"ai_ollama","healthy":false,"status":500,"responseTimeMs":250,"timestamp":"2026-06-14T12:00:00Z","failedConditions":["[STATUS] == 200"],"errors":["connection refused"]}`
	pendingJSON = `{"name":"New","group":"","key":"new","healthy":null,"note":"no probe results yet"}`
)

func mockServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	serve := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			if _, err := io.WriteString(w, body); err != nil {
				t.Errorf("write response: %v", err)
			}
		}
	}
	mux.HandleFunc("/api/v1/endpoints/statuses", serve(statusesJSON))
	mux.HandleFunc("/api/v1/endpoints/infra_planka/statuses", serve(historyJSON))
	mux.HandleFunc("/api/v1/endpoints/no_errors/statuses", serve(`{"name":"X","key":"no_errors","results":[{"status":200,"success":true,"duration":1000000,"timestamp":"t"}]}`))
	mux.HandleFunc("/api/v1/endpoints/infra_planka/uptimes/24h", serve("0.9987"))
	mux.HandleFunc("/api/v1/endpoints/infra_planka/uptimes/1h", serve("not-a-number"))
	mux.HandleFunc("/api/v1/endpoints/infra_planka/response-times/24h", serve("42"))
	mux.HandleFunc("/api/v1/endpoints/infra_planka/response-times/24h/history", serve(`[{"timestamp":"2026-06-14T12:00:00Z","average":42}]`))
	mux.HandleFunc("/api/v1/endpoints/infra_planka/health/badge.svg", serve("<svg>up</svg>"))
	mux.HandleFunc("/api/v1/endpoints/infra_planka/health/badge.shields", serve(`{"schemaVersion":1,"label":"health","message":"healthy","color":"green"}`))
	mux.HandleFunc("/api/v1/endpoints/infra_planka/uptimes/24h/badge.svg", serve("<svg>uptime</svg>"))
	mux.HandleFunc("/api/v1/endpoints/infra_planka/response-times/24h/badge.svg", serve("<svg>rt</svg>"))
	mux.HandleFunc("/api/v1/endpoints/infra_planka/response-times/24h/chart.svg", serve("<svg>chart</svg>"))
	mux.HandleFunc("/api/v1/suites/statuses", serve(`[{"name":"suite1","key":"suite1"}]`))
	mux.HandleFunc("/api/v1/suites/suite1/statuses", serve(`{"name":"suite1","key":"suite1"}`))
	mux.HandleFunc("/api/v1/endpoints/ext_ep/external", serve("result submitted"))
	mux.HandleFunc("/api/v1/config", serve(`{"oidc":false,"authenticated":false}`))
	mux.HandleFunc("/health", serve(`{"status":"UP"}`))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func mockGatus(t *testing.T) *toolset {
	t.Helper()
	return &toolset{client: gatus.NewClient(mockServer(t).URL, "")}
}

// brokenGatus returns a toolset whose client points at a closed server, so every
// request fails with a connection error.
func brokenGatus(t *testing.T) *toolset {
	t.Helper()
	srv := httptest.NewServer(http.NewServeMux())
	srv.Close()
	return &toolset{client: gatus.NewClient(srv.URL, "tok")}
}

func assertJSON(t *testing.T, got any, wantJSON string) {
	t.Helper()
	gotBytes, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal got: %v", err)
	}
	var gotAny, wantAny any
	if err := json.Unmarshal(gotBytes, &gotAny); err != nil {
		t.Fatalf("unmarshal got: %v", err)
	}
	if err := json.Unmarshal([]byte(wantJSON), &wantAny); err != nil {
		t.Fatalf("unmarshal want: %v", err)
	}
	if !reflect.DeepEqual(gotAny, wantAny) {
		t.Errorf("JSON mismatch\n got: %s\nwant: %s", gotBytes, wantJSON)
	}
}

func TestHealthSummary(t *testing.T) {
	ts := mockGatus(t)
	_, out, err := ts.healthSummary(t.Context(), nil, emptyInput{})
	if err != nil {
		t.Fatal(err)
	}
	assertJSON(t, out, `{"total":3,"up":1,"down":1,"pending":1,"unhealthy":[`+downJSON+`]}`)
}

func TestListStatuses(t *testing.T) {
	ts := mockGatus(t)
	cases := []struct {
		name string
		in   listInput
		want string
	}{
		{"all", listInput{}, `{"result":[` + healthyJSON + `,` + downJSON + `,` + pendingJSON + `]}`},
		{"only_unhealthy", listInput{OnlyUnhealthy: true}, `{"result":[` + downJSON + `]}`},
		{"group", listInput{Group: "infra"}, `{"result":[` + healthyJSON + `]}`},
		{"group_case_insensitive", listInput{Group: "AI"}, `{"result":[` + downJSON + `]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, out, err := ts.listStatuses(t.Context(), nil, tc.in)
			if err != nil {
				t.Fatal(err)
			}
			assertJSON(t, out, tc.want)
		})
	}
}

func TestEndpointHistory(t *testing.T) {
	ts := mockGatus(t)
	_, out, err := ts.endpointHistory(t.Context(), nil, historyInput{Key: "infra_planka"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"name":"Planka","group":"infra","key":"infra_planka","results":[` +
		`{"status":200,"success":true,"responseTimeMs":12.3,"timestamp":"2026-06-14T12:00:00Z","errors":[]},` +
		`{"status":500,"success":false,"responseTimeMs":99,"timestamp":"2026-06-14T12:00:30Z","errors":["boom"]}` +
		`]}`
	assertJSON(t, out, want)
}

func TestEndpointHistoryRequiresKey(t *testing.T) {
	ts := mockGatus(t)
	if _, _, err := ts.endpointHistory(t.Context(), nil, historyInput{}); err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestRawMetrics(t *testing.T) {
	ts := mockGatus(t)
	_, up, err := ts.endpointUptime(t.Context(), nil, metricInput{Key: "infra_planka", Duration: "24h"})
	if err != nil {
		t.Fatal(err)
	}
	assertJSON(t, up, `{"key":"infra_planka","duration":"24h","value":0.9987}`)

	_, rt, err := ts.endpointResponseTime(t.Context(), nil, metricInput{Key: "infra_planka", Duration: "24h"})
	if err != nil {
		t.Fatal(err)
	}
	assertJSON(t, rt, `{"key":"infra_planka","duration":"24h","value":42}`)
}

func TestRawMetricNonNumeric(t *testing.T) {
	ts := mockGatus(t)
	if _, _, err := ts.endpointUptime(t.Context(), nil, metricInput{Key: "infra_planka", Duration: "1h"}); err == nil {
		t.Fatal("expected error for non-numeric body")
	}
}

func TestBadDurationRejected(t *testing.T) {
	ts := mockGatus(t)
	if _, _, err := ts.endpointUptime(t.Context(), nil, metricInput{Key: "infra_planka", Duration: "5m"}); err == nil {
		t.Fatal("expected error for invalid duration")
	}
	if _, _, err := ts.uptimeBadge(t.Context(), nil, metricInput{Key: "infra_planka", Duration: "5m"}); err == nil {
		t.Fatal("expected error for invalid duration on badge")
	}
}

func TestResponseTimeHistory(t *testing.T) {
	ts := mockGatus(t)
	_, out, err := ts.responseTimeHistory(t.Context(), nil, metricInput{Key: "infra_planka", Duration: "24h"})
	if err != nil {
		t.Fatal(err)
	}
	assertJSON(t, out, `[{"timestamp":"2026-06-14T12:00:00Z","average":42}]`)
}

func TestResponseTimeHistoryValidation(t *testing.T) {
	ts := mockGatus(t)
	if _, _, err := ts.responseTimeHistory(t.Context(), nil, metricInput{Duration: "24h"}); err == nil {
		t.Error("expected error for missing key")
	}
	if _, _, err := ts.responseTimeHistory(t.Context(), nil, metricInput{Key: "infra_planka", Duration: "5m"}); err == nil {
		t.Error("expected error for invalid duration")
	}
}

func TestBadges(t *testing.T) {
	ts := mockGatus(t)
	cases := []struct {
		name string
		call func() (svgOutput, error)
		want string
	}{
		{"health", func() (svgOutput, error) {
			_, o, e := ts.healthBadge(t.Context(), nil, svgInput{Key: "infra_planka"})
			return o, e
		}, `{"key":"infra_planka","svg":"<svg>up</svg>"}`},
		{"uptime", func() (svgOutput, error) {
			_, o, e := ts.uptimeBadge(t.Context(), nil, metricInput{Key: "infra_planka", Duration: "24h"})
			return o, e
		}, `{"key":"infra_planka","duration":"24h","svg":"<svg>uptime</svg>"}`},
		{"response_time", func() (svgOutput, error) {
			_, o, e := ts.responseTimeBadge(t.Context(), nil, metricInput{Key: "infra_planka", Duration: "24h"})
			return o, e
		}, `{"key":"infra_planka","duration":"24h","svg":"<svg>rt</svg>"}`},
		{"chart", func() (svgOutput, error) {
			_, o, e := ts.responseTimeChart(t.Context(), nil, metricInput{Key: "infra_planka", Duration: "24h"})
			return o, e
		}, `{"key":"infra_planka","duration":"24h","svg":"<svg>chart</svg>"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := tc.call()
			if err != nil {
				t.Fatal(err)
			}
			assertJSON(t, out, tc.want)
		})
	}
}

func TestHealthBadgeShields(t *testing.T) {
	ts := mockGatus(t)
	_, out, err := ts.healthBadgeShields(t.Context(), nil, svgInput{Key: "infra_planka"})
	if err != nil {
		t.Fatal(err)
	}
	assertJSON(t, out, `{"schemaVersion":1,"label":"health","message":"healthy","color":"green"}`)
}

func TestSuites(t *testing.T) {
	ts := mockGatus(t)
	_, list, err := ts.suiteStatuses(t.Context(), nil, emptyInput{})
	if err != nil {
		t.Fatal(err)
	}
	assertJSON(t, list, `[{"name":"suite1","key":"suite1"}]`)

	_, one, err := ts.suiteStatus(t.Context(), nil, suiteInput{Key: "suite1"})
	if err != nil {
		t.Fatal(err)
	}
	assertJSON(t, one, `{"name":"suite1","key":"suite1"}`)

	if _, _, err := ts.suiteStatus(t.Context(), nil, suiteInput{}); err == nil {
		t.Fatal("expected error for missing suite key")
	}
}

func TestPassthrough(t *testing.T) {
	ts := mockGatus(t)
	_, cfg, err := ts.config(t.Context(), nil, emptyInput{})
	if err != nil {
		t.Fatal(err)
	}
	assertJSON(t, cfg, `{"oidc":false,"authenticated":false}`)

	_, live, err := ts.liveness(t.Context(), nil, emptyInput{})
	if err != nil {
		t.Fatal(err)
	}
	assertJSON(t, live, `{"status":"UP"}`)
}

func TestSubmitExternalRequiresToken(t *testing.T) {
	ts := mockGatus(t)
	if _, _, err := ts.submitExternal(t.Context(), nil, externalInput{Key: "x", Success: true}); err == nil {
		t.Fatal("expected error when GATUS_TOKEN is unset")
	}
}

func TestSubmitExternalSuccess(t *testing.T) {
	ts := &toolset{client: gatus.NewClient(mockServer(t).URL, "tok")}
	_, out, err := ts.submitExternal(t.Context(), nil, externalInput{Key: "ext_ep", Success: true})
	if err != nil {
		t.Fatal(err)
	}
	assertJSON(t, out, `{"response":"result submitted"}`)

	if _, _, err := ts.submitExternal(t.Context(), nil, externalInput{Key: ""}); err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestEndpointPathEscapesKey(t *testing.T) {
	got := endpointPath("a/b?c#d", "statuses")
	want := "/api/v1/endpoints/a%2Fb%3Fc%23d/statuses"
	if got != want {
		t.Errorf("endpointPath = %q, want %q", got, want)
	}
	if endpointPath("k") != "/api/v1/endpoints/k" {
		t.Errorf("endpointPath with no segments = %q", endpointPath("k"))
	}
}

func TestRoundMs(t *testing.T) {
	cases := []struct {
		ns   int64
		want float64
	}{
		{12340000, 12.3}, // rounds down
		{12360000, 12.4}, // rounds up (a truncating impl would give 12.3)
		{44000, 0},       // 0.044ms -> 0.0
		{60000, 0.1},     // 0.06ms  -> 0.1 (truncation would give 0.0)
		{250000000, 250}, // whole number
		{0, 0},           // no duration
	}
	for _, tc := range cases {
		if got := roundMs(tc.ns); got != tc.want {
			t.Errorf("roundMs(%d) = %v, want %v", tc.ns, got, tc.want)
		}
	}
}

func TestValidateDuration(t *testing.T) {
	for _, d := range []string{"1h", "24h", "7d", "30d"} {
		if err := validateDuration(d); err != nil {
			t.Errorf("validateDuration(%q) = %v, want nil", d, err)
		}
	}
	for _, d := range []string{"5m", "", "1H", "24", "1d"} {
		if validateDuration(d) == nil {
			t.Errorf("validateDuration(%q) = nil, want error", d)
		}
	}
}

func TestEndpointHistoryNormalizesNilErrors(t *testing.T) {
	ts := mockGatus(t)
	_, out, err := ts.endpointHistory(t.Context(), nil, historyInput{Key: "no_errors"})
	if err != nil {
		t.Fatal(err)
	}
	// A result with no "errors" key must serialize as [] (not null), and the
	// requested key must be echoed back when Gatus omits it.
	assertJSON(t, out, `{"name":"X","group":"","key":"no_errors","results":[`+
		`{"status":200,"success":true,"responseTimeMs":1,"timestamp":"t","errors":[]}]}`)
}

func TestUpstreamErrorMessageIsWrapped(t *testing.T) {
	ts := brokenGatus(t)
	_, _, err := ts.healthSummary(t.Context(), nil, emptyInput{})
	if err == nil {
		t.Fatal("expected an error when Gatus is unreachable")
	}
	if !strings.Contains(err.Error(), "gatus request failed") {
		t.Errorf("error not wrapped with context: %v", err)
	}
}

// TestUpstreamErrorsPropagate exercises the error-return branch of every handler
// when Gatus is unreachable.
func TestUpstreamErrorsPropagate(t *testing.T) {
	ts := brokenGatus(t)
	ctx := t.Context()
	m := metricInput{Key: "k", Duration: "24h"}
	calls := map[string]func() error{
		"healthSummary":        func() error { _, _, e := ts.healthSummary(ctx, nil, emptyInput{}); return e },
		"listStatuses":         func() error { _, _, e := ts.listStatuses(ctx, nil, listInput{}); return e },
		"endpointHistory":      func() error { _, _, e := ts.endpointHistory(ctx, nil, historyInput{Key: "k"}); return e },
		"endpointUptime":       func() error { _, _, e := ts.endpointUptime(ctx, nil, m); return e },
		"endpointResponseTime": func() error { _, _, e := ts.endpointResponseTime(ctx, nil, m); return e },
		"responseTimeHistory":  func() error { _, _, e := ts.responseTimeHistory(ctx, nil, m); return e },
		"healthBadge":          func() error { _, _, e := ts.healthBadge(ctx, nil, svgInput{Key: "k"}); return e },
		"uptimeBadge":          func() error { _, _, e := ts.uptimeBadge(ctx, nil, m); return e },
		"responseTimeBadge":    func() error { _, _, e := ts.responseTimeBadge(ctx, nil, m); return e },
		"responseTimeChart":    func() error { _, _, e := ts.responseTimeChart(ctx, nil, m); return e },
		"healthBadgeShields":   func() error { _, _, e := ts.healthBadgeShields(ctx, nil, svgInput{Key: "k"}); return e },
		"suiteStatuses":        func() error { _, _, e := ts.suiteStatuses(ctx, nil, emptyInput{}); return e },
		"suiteStatus":          func() error { _, _, e := ts.suiteStatus(ctx, nil, suiteInput{Key: "k"}); return e },
		"config":               func() error { _, _, e := ts.config(ctx, nil, emptyInput{}); return e },
		"liveness":             func() error { _, _, e := ts.liveness(ctx, nil, emptyInput{}); return e },
		"submitExternal":       func() error { _, _, e := ts.submitExternal(ctx, nil, externalInput{Key: "k"}); return e },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			if err := call(); err == nil {
				t.Errorf("%s: expected error when Gatus is unreachable", name)
			}
		})
	}
}
