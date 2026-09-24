package discover

import (
	"os"
	"path/filepath"
	"testing"
)

const sessionUUID = "0f5c2a8e-1b2c-4d5e-8f90-123456789abc"

func writeFixture(t *testing.T, root, relative string) string {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return full
}

func TestSessionsFindsMainAndSubagentTranscriptsOnly(t *testing.T) {
	root := t.TempDir()
	main := writeFixture(t, root, "projects/p/"+sessionUUID+".jsonl")
	agent := writeFixture(t, root, "projects/p/"+sessionUUID+"/subagents/agent-a1.jsonl")
	workflow := writeFixture(t, root, "projects/p/"+sessionUUID+"/subagents/workflows/wf_1/agent-b.jsonl")
	writeFixture(t, root, "projects/p/"+sessionUUID+"/subagents/workflows/wf_1/journal.jsonl")
	writeFixture(t, root, "projects/p/memory/x.md")
	writeFixture(t, root, "projects/p/"+sessionUUID+"/tool-results/x.txt")
	writeFixture(t, root, ".credentials.json")
	writeFixture(t, root, "history.jsonl")
	writeFixture(t, root, "todos/elsewhere.jsonl")

	sources, err := Sessions(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{main: false, agent: true, workflow: true}
	if len(sources) != len(want) {
		t.Fatalf("sources=%+v", sources)
	}
	for _, source := range sources {
		subagent, ok := want[source.Path]
		if !ok || source.Subagent != subagent {
			t.Fatalf("unexpected source %+v", source)
		}
	}
	for index := 1; index < len(sources); index++ {
		if sources[index-1].Path > sources[index].Path {
			t.Fatalf("sources not sorted: %+v", sources)
		}
	}
}

func TestSessionsIgnoresSubagentsNamedProjectDirectory(t *testing.T) {
	root := t.TempDir()
	path := writeFixture(t, root, "projects/subagents/"+sessionUUID+".jsonl")
	sources, err := Sessions(root)
	if err != nil || len(sources) != 1 || sources[0].Path != path || sources[0].Subagent {
		t.Fatalf("sources=%+v err=%v", sources, err)
	}
}

func TestSessionsSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFixture(t, outside, "secret/leak.jsonl")
	target := writeFixture(t, outside, "file.jsonl")
	if err := os.MkdirAll(filepath.Join(root, "projects", "p"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "projects", "p", "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(root, "projects", "p", "linked.jsonl")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	sources, err := Sessions(root)
	if err != nil || len(sources) != 0 {
		t.Fatalf("followed a symlink: sources=%+v err=%v", sources, err)
	}
}

func TestHomeHonorsEnvironmentAndDefault(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root+string(filepath.Separator)+".")
	home, err := Home()
	if err != nil || home != root {
		t.Fatalf("home=%s err=%v", home, err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	userHome, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no user home directory")
	}
	home, err = Home()
	if err != nil || home != filepath.Join(userHome, ".claude") {
		t.Fatalf("home=%s err=%v", home, err)
	}
}

func TestSessionsMissingProjectsDirectory(t *testing.T) {
	sources, err := Sessions(filepath.Join(t.TempDir(), "missing"))
	if err != nil || len(sources) != 0 {
		t.Fatalf("sources=%v err=%v", sources, err)
	}
}

func TestSessionsDeepNonSubagentPathAndWalkErrors(t *testing.T) {
	root := t.TempDir()
	path := writeFixture(t, root, "projects/p/"+sessionUUID+"/nested/other.jsonl")
	sources, err := Sessions(root)
	if err != nil || len(sources) != 1 || sources[0].Path != path || sources[0].Subagent {
		t.Fatalf("sources=%+v err=%v", sources, err)
	}
	if _, err := Sessions(root + "\x00invalid"); err == nil {
		t.Fatal("expected an error for an invalid root")
	}
}

func TestHomeReportsMissingUserHome(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("HOME", "")
	t.Setenv("home", "")
	if home, err := Home(); err == nil {
		t.Fatalf("expected an error without a home directory, got %q", home)
	}
}

func TestIsSubagentRejectsForeignPaths(t *testing.T) {
	if isSubagent(filepath.Join("a", "projects"), filepath.Join("b", "subagents", "x", "y.jsonl")) {
		t.Fatal("path outside base must not be a subagent")
	}
	if isSubagent(t.TempDir(), filepath.Join("relative", "subagents", "x", "y.jsonl")) {
		t.Fatal("unrelatable path must not be a subagent")
	}
}
