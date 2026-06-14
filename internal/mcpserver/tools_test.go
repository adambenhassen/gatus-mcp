package mcpserver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
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

func mockGatus(t *testing.T) *toolset {
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
	mux.HandleFunc("/api/v1/endpoints/infra_planka/uptimes/24h", serve("0.9987"))
	mux.HandleFunc("/api/v1/endpoints/infra_planka/response-times/24h", serve("42"))
	mux.HandleFunc("/api/v1/endpoints/infra_planka/health/badge.svg", serve("<svg>up</svg>"))
	mux.HandleFunc("/api/v1/config", serve(`{"oidc":false,"authenticated":false}`))
	mux.HandleFunc("/health", serve(`{"status":"UP"}`))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &toolset{client: gatus.NewClient(srv.URL, "")}
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

func TestBadDurationRejected(t *testing.T) {
	ts := mockGatus(t)
	if _, _, err := ts.endpointUptime(t.Context(), nil, metricInput{Key: "infra_planka", Duration: "5m"}); err == nil {
		t.Fatal("expected error for invalid duration")
	}
}

func TestHealthBadgeSVG(t *testing.T) {
	ts := mockGatus(t)
	_, out, err := ts.healthBadge(t.Context(), nil, svgInput{Key: "infra_planka"})
	if err != nil {
		t.Fatal(err)
	}
	assertJSON(t, out, `{"key":"infra_planka","svg":"<svg>up</svg>"}`)
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
