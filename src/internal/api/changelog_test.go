// Package api tests the local semantic changelog endpoint.
// @module ChangelogAPITests anchors bounded release-history responses.
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LanceSandino/sessionsift/src/internal/release"
)

func TestChangelogEndpointPaginatesAndRejectsInvalidPages(t *testing.T) {
	store := testStore(t)
	handler := Handler(store)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, localRequest(http.MethodGet, "/api/v1/changelog?page=1&page_size=1", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	var page release.PageResult
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.CurrentVersion != "2.1.0" || len(page.Releases) != 1 || !page.HasNext {
		t.Fatalf("unexpected page: %#v", page)
	}

	for _, target := range []string{"/api/v1/changelog?page=0", "/api/v1/changelog?page=1&page_size=21", "/api/v1/changelog?page=99&page_size=1"} {
		invalid := httptest.NewRecorder()
		handler.ServeHTTP(invalid, localRequest(http.MethodGet, target, nil))
		if invalid.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d", target, invalid.Code)
		}
	}
}
