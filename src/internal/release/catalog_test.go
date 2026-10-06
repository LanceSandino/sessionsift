// Package release tests the semantic release catalog and bounded pagination.
// @module ReleaseCatalogTests anchors version consistency and changelog paging.
package release

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestCatalogUsesSemanticVersionsAndMatchesHumanChangelog(t *testing.T) {
	catalog := Catalog()
	if catalog.CurrentVersion != "2.0.0" {
		t.Fatalf("current version = %q, want 2.0.0", catalog.CurrentVersion)
	}
	semantic := regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	if len(catalog.Releases) < 2 {
		t.Fatalf("release count = %d, want at least current and initial releases", len(catalog.Releases))
	}
	for _, item := range catalog.Releases {
		if !semantic.MatchString(item.Version) {
			t.Fatalf("release version %q is not SemVer", item.Version)
		}
		if item.Date == "" || item.Title == "" || len(item.Changes) == 0 {
			t.Fatalf("incomplete release: %#v", item)
		}
	}
	body, err := os.ReadFile("../../../CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range catalog.Releases {
		if !strings.Contains(string(body), "## ["+item.Version+"]") {
			t.Fatalf("CHANGELOG.md does not contain release %s", item.Version)
		}
	}
}

func TestPageBoundsReleaseHistory(t *testing.T) {
	first, err := Page(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.Page != 1 || first.PageSize != 1 || first.Total < 2 || len(first.Releases) != 1 || !first.HasNext || first.HasPrevious {
		t.Fatalf("unexpected first page: %#v", first)
	}
	second, err := Page(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if second.Page != 2 || len(second.Releases) != 1 || !second.HasPrevious {
		t.Fatalf("unexpected second page: %#v", second)
	}
	for _, invalid := range [][2]int{{0, 1}, {1, 0}, {1, 21}, {99, 1}} {
		if _, err = Page(invalid[0], invalid[1]); err == nil {
			t.Fatalf("Page(%d, %d) succeeded", invalid[0], invalid[1])
		}
	}
}

func TestCatalogAndPagesReturnIndependentReleaseCopies(t *testing.T) {
	catalog := Catalog()
	page, err := Page(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	catalog.Releases[0].Changes[0].Items[0] = "mutated catalog"
	page.Releases[0].Changes[0].Items[0] = "mutated page"

	fresh := Catalog()
	if got := fresh.Releases[0].Changes[0].Items[0]; got == "mutated catalog" || got == "mutated page" {
		t.Fatalf("release catalog leaked caller mutation: %q", got)
	}
}
