// Package release owns SessionSift's semantic version and public release notes.
// @module ReleaseCatalog provides bounded, newest-first changelog pages.
package release

import "fmt"

const currentVersion = "2.0.0"

// ChangeGroup collects related user-visible changes within a release.
type ChangeGroup struct {
	Category string   `json:"category"`
	Items    []string `json:"items"`
}

// Release is one immutable semantic release record.
type Release struct {
	Version string        `json:"version"`
	Date    string        `json:"date"`
	Title   string        `json:"title"`
	Changes []ChangeGroup `json:"changes"`
}

// ReleaseCatalog is the complete newest-first release history.
type ReleaseCatalog struct {
	CurrentVersion string    `json:"current_version"`
	Releases       []Release `json:"releases"`
}

// PageResult is one bounded changelog response.
type PageResult struct {
	CurrentVersion string    `json:"current_version"`
	Page           int       `json:"page"`
	PageSize       int       `json:"page_size"`
	Total          int       `json:"total"`
	TotalPages     int       `json:"total_pages"`
	HasPrevious    bool      `json:"has_previous"`
	HasNext        bool      `json:"has_next"`
	Releases       []Release `json:"releases"`
}

var releases = []Release{
	{
		Version: "2.0.0",
		Date:    "2026-10-05",
		Title:   "Stable Docker service and visible release history",
		Changes: []ChangeGroup{
			{Category: "Added", Items: []string{"Docker Compose services for the Local Agent and hosted bridge with persistent existing state, bounded health checks, and automatic restart.", "A paginated in-app changelog backed by the same semantic release catalog as the CLI version."}},
			{Category: "Changed", Items: []string{"Service operations now use Docker Compose on macOS instead of project-managed launchd agents.", "Protocol and file reads observed in Codex tool calls are visible as provenance badges.", "Bounded full-session reads support long-running conversations without removing response ceilings."}},
		},
	},
	{Version: "1.8.0", Date: "2026-09-25", Title: "Personal optimization and session provenance", Changes: []ChangeGroup{{Category: "Added", Items: []string{"A private recommendation, experiment, measurement, and weekly-report loop.", "Session ownership filters and evidence-qualified protocol, Blueprint, memory, and file-read callouts."}}, {Category: "Fixed", Items: []string{"Oversized transcript records recover without hiding later records.", "Analytics inventory and diagnostics distinguish deliberate exclusions from parser loss."}}}},
	{Version: "1.7.0", Date: "2026-09-24", Title: "Conversation-first workspace", Changes: []ChangeGroup{{Category: "Changed", Items: []string{"Rebuilt local and shared-session interfaces around Conversation, Timeline, Tools, Review, and Evidence views.", "Mobile discovery, timeline containment, card sizing, and latest-navigation behavior are responsive to their actual containers."}}, {Category: "Fixed", Items: []string{"Codex turn identities are session-scoped and per-session import failures no longer stop later sessions."}}}},
	{Version: "1.6.0", Date: "2026-09-23", Title: "Claude parity and support codes", Changes: []ChangeGroup{{Category: "Added", Items: []string{"Claude Code transcript discovery and Claude.ai export import under the shared redaction model.", "Device-generated single-session support codes with view, comment, or review roles."}}, {Category: "Security", Items: []string{"Keyed path hashes, bounded hook metadata, fail-closed local state reloads, and explicit owner authorization remain independent controls."}}}},
	{Version: "1.5.0", Date: "2026-09-20", Title: "Private collaboration beta", Changes: []ChangeGroup{{Category: "Added", Items: []string{"Hosted bridge, OAuth-backed MCP, email-bound invitations, pairing, grants, annotations, and review decisions.", "Envelope-encrypted transient relay tasks, metadata-only audit, paired bridge backups, and recovery checks."}}, {Category: "Security", Items: []string{"Access identity never replaces Local Agent capability grants; public routes fail closed without origin verification."}}}},
	{Version: "1.4.0", Date: "2026-09-20", Title: "Analytics and resilient operation", Changes: []ChangeGroup{{Category: "Added", Items: []string{"Privacy-safe aggregate analytics, source/model/tool breakdowns, and deterministic recommendations.", "SQLite readiness, private retained backups, bounded HTTP timeouts, and supervised restart templates."}}, {Category: "Fixed", Items: []string{"Interactive reads remain responsive during ingestion and session lists page beyond the first 25 records."}}}},
	{Version: "1.3.0", Date: "2026-09-20", Title: "Responsive evidence workspace", Changes: []ChangeGroup{{Category: "Changed", Items: []string{"Introduced viewport-safe desktop, tablet, phone, focus, timeline, and inspector layouts.", "Timeline checkpoints summarize the work journey while exact redacted calls stay available in Tools."}}, {Category: "Fixed", Items: []string{"Safari overlap, black refresh, nullable collection, and long-timeline navigation failures."}}}},
	{Version: "1.2.0", Date: "2026-09-20", Title: "Local Agent and restricted MCP", Changes: []ChangeGroup{{Category: "Added", Items: []string{"A canonical Local Agent and allowlisted Session Protocol shared by CLI, UI, local MCP, and remote adapters.", "Modern stdio MCP tools with bounded redacted responses and no generic shell, filesystem, process, or SQL surface."}}}},
	{Version: "1.1.0", Date: "2026-09-20", Title: "Chat imports and exact tool evidence", Changes: []ChangeGroup{{Category: "Added", Items: []string{"Explicit ChatGPT export import with incremental replacement reconciliation.", "Exact built-in, MCP, and app tool reporting, including nested orchestration calls.", "Renamed titles, execution context, deep links, lifecycle hooks, and OpenTelemetry capture."}}}},
	{
		Version: "1.0.0",
		Date:    "2026-09-20",
		Title:   "Initial local-first release",
		Changes: []ChangeGroup{
			{Category: "Added", Items: []string{"Local-first Codex and Claude Code transcript analysis with explicit ChatGPT and Claude.ai export import.", "Privacy-safe session, timeline, tool, analytics, optimization, support, invitation, bridge, and MCP workflows.", "Redacted SQLite storage, live ingestion, diagnostics, backups, and supervised deployment templates."}},
		},
	},
}

// CurrentVersion returns the SemVer shared by the CLI, API and interface.
func CurrentVersion() string { return currentVersion }

// Catalog returns a copy so callers cannot mutate process-wide release history.
func Catalog() ReleaseCatalog {
	items := cloneReleases(releases)
	return ReleaseCatalog{CurrentVersion: currentVersion, Releases: items}
}

// Page returns a bounded 1-based slice of the release catalog.
func Page(page, pageSize int) (PageResult, error) {
	if page < 1 || pageSize < 1 || pageSize > 20 {
		return PageResult{}, fmt.Errorf("page must be positive and page_size must be between 1 and 20")
	}
	total := len(releases)
	totalPages := (total + pageSize - 1) / pageSize
	if page > totalPages {
		return PageResult{}, fmt.Errorf("page %d exceeds %d available pages", page, totalPages)
	}
	start := (page - 1) * pageSize
	end := min(start+pageSize, total)
	items := cloneReleases(releases[start:end])
	return PageResult{CurrentVersion: currentVersion, Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages, HasPrevious: page > 1, HasNext: page < totalPages, Releases: items}, nil
}

func cloneReleases(source []Release) []Release {
	items := make([]Release, len(source))
	for i, item := range source {
		items[i] = item
		items[i].Changes = make([]ChangeGroup, len(item.Changes))
		for j, group := range item.Changes {
			items[i].Changes[j] = group
			items[i].Changes[j].Items = append([]string(nil), group.Items...)
		}
	}
	return items
}
