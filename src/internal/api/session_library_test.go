// @module SessionLibraryTests verifies complete paginated libraries and standalone detail metadata.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/LanceSandino/sessionsift/src/internal/agent/service"
	"github.com/LanceSandino/sessionsift/src/internal/domain"
)

func TestLocalLibraryPaginationAndStandaloneMetrics(t *testing.T) {
	s := testStore(t)
	for i := 0; i < 29; i++ {
		id := fmt.Sprintf("page-%d", i)
		if err := s.SaveImport(context.Background(), id, "one", domain.Session{ID: id, ExternalSessionID: id}); err != nil {
			t.Fatal(err)
		}
	}
	h := Handler(s)
	for _, tc := range []struct {
		query  string
		count  int
		status int
	}{{"?limit=100", 30, 200}, {"?limit=10&cursor=20", 10, 200}, {"?cursor=bad", 0, 400}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, localRequest("GET", "/api/v1/sessions"+tc.query, nil))
		if w.Code != tc.status {
			t.Fatalf("%s status=%d", tc.query, w.Code)
		}
		if tc.status == 200 {
			var values []service.SessionSummary
			if err := json.Unmarshal(w.Body.Bytes(), &values); err != nil || len(values) != tc.count {
				t.Fatalf("%s count=%d err=%v", tc.query, len(values), err)
			}
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, localRequest("GET", "/api/v1/sessions/s1", nil))
	var detail service.SessionSummary
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil || detail.Metrics.TurnCount != 1 {
		t.Fatalf("detail lacks independent metrics: %v", err)
	}
}
