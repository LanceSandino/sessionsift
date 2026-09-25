package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"github.com/LanceSandino/sessionsift/src/internal/optimize"
)

func TestOptimizationLoopRoutesCreateMeasureDecideAndExport(t *testing.T) {
	store := testStore(t)
	now := time.Now().UTC()
	followup := domain.Session{ID: "s2", ExternalSessionID: "s2", Title: "Follow-up", Kind: domain.DeveloperSession, Source: domain.SourceCodex, StartedAt: now, EndedAt: now.Add(time.Minute), ParserVersion: domain.ParserVersion, Turns: []domain.Turn{{ID: "t2", SessionID: "s2", InputTokens: 60}}}
	if err := store.SaveImport(context.Background(), "path2", "fingerprint", followup); err != nil {
		t.Fatal(err)
	}
	handler := Handler(store)

	created := performOptimizationRequest(t, handler, http.MethodPost, "/api/v1/optimization/experiments", `{"title":"Validate earlier","hypothesis":"Reduce retries","baseline_session_id":"s1"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	var experiment optimize.Experiment
	if err := json.Unmarshal(created.Body.Bytes(), &experiment); err != nil {
		t.Fatal(err)
	}
	if experiment.ID == "" || experiment.Status != optimize.ExperimentRunning {
		t.Fatalf("experiment=%+v", experiment)
	}

	updated := performOptimizationRequest(t, handler, http.MethodPost, "/api/v1/optimization/experiments/update", `{"id":"`+experiment.ID+`","title":"Validate earlier","hypothesis":"Reduce retries","baseline_session_id":"s1","followup_session_id":"s2"}`)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"status":"measured"`) {
		t.Fatalf("update=%d %s", updated.Code, updated.Body.String())
	}
	decision := performOptimizationRequest(t, handler, http.MethodPost, "/api/v1/optimization/recommendations", `{"recommendation_id":"tool-failures","state":"applied"}`)
	if decision.Code != http.StatusOK {
		t.Fatalf("decision=%d %s", decision.Code, decision.Body.String())
	}

	overview := performOptimizationRequest(t, handler, http.MethodGet, "/api/v1/optimization", "")
	if overview.Code != http.StatusOK || overview.Header().Get("Cache-Control") != "no-store" || !strings.Contains(overview.Body.String(), `"measurement"`) || strings.Contains(overview.Body.String(), "sensitive prompt") {
		t.Fatalf("overview=%d %s", overview.Code, overview.Body.String())
	}
	markdown := performOptimizationRequest(t, handler, http.MethodGet, "/api/v1/optimization/report.md", "")
	if markdown.Code != http.StatusOK || !strings.HasPrefix(markdown.Header().Get("Content-Type"), "text/markdown") || !strings.Contains(markdown.Body.String(), "Observed deltas") {
		t.Fatalf("markdown=%d headers=%v body=%s", markdown.Code, markdown.Header(), markdown.Body.String())
	}
	if markdown.Header().Get("Content-Disposition") == "" {
		t.Fatal("markdown export is not downloadable")
	}
	jsonReport := performOptimizationRequest(t, handler, http.MethodGet, "/api/v1/optimization/report.json", "")
	if jsonReport.Code != http.StatusOK || jsonReport.Header().Get("Content-Disposition") == "" {
		t.Fatalf("json report=%d headers=%v", jsonReport.Code, jsonReport.Header())
	}
	if strings.Contains(jsonReport.Body.String(), "baseline_session_id") || strings.Contains(jsonReport.Body.String(), "Baseline session") {
		t.Fatalf("safe export leaked session identity: %s", jsonReport.Body.String())
	}
}

func TestOptimizationRoutesRejectInvalidSessionAndPayload(t *testing.T) {
	handler := Handler(testStore(t))
	for _, test := range []struct{ path, body string }{
		{"/api/v1/optimization/experiments", `{"title":"Missing","baseline_session_id":"unknown"}`},
		{"/api/v1/optimization/experiments", `{"title":"Same","baseline_session_id":"s1","followup_session_id":"s1"}`},
		{"/api/v1/optimization/recommendations", `{"recommendation_id":"x","state":"invalid"}`},
		{"/api/v1/optimization/adoption", `{"event":"prompt_body"}`},
	} {
		response := performOptimizationRequest(t, handler, http.MethodPost, test.path, test.body)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s=%d %s", test.path, response.Code, response.Body.String())
		}
	}
}

func performOptimizationRequest(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	request := localRequest(method, path, reader)
	handler.ServeHTTP(response, request)
	return response
}
