// @module BridgeOperationsTests anchors private backups, bounded storage and safe public assets.
package bridge

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestQueueAndEnrollmentBudgetsAreBounded(t *testing.T) {
	t.Run("CancellationCleanup", runWaitCancellationCleanup)
	s, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	device := enrollTenantDevice(t, s, "owner@example.com", "Laptop")
	_, err = s.RegisterGrant(device.ID, "owner@example.com", []protocol.Operation{protocol.SessionList}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 16; i++ {
		if _, err = s.Submit("owner@example.com", device.ID, protocol.Request{Version: 1, RequestID: "budget", Operation: protocol.SessionList, Arguments: json.RawMessage(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.Submit("owner@example.com", device.ID, protocol.Request{Version: 1, RequestID: "over-budget", Operation: protocol.SessionList, Arguments: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("queue unbounded")
	}
	h := newHandler(s, nil, "", nil)
	for i := 0; i < 4096; i++ {
		h.attempts[string(rune(i))] = attemptWindow{started: time.Now()}
	}
	if h.allowAttempt("new-source") {
		t.Fatal("enrollment map unbounded")
	}
	for key := range h.attempts {
		h.attempts[key] = attemptWindow{started: time.Now().Add(-time.Hour)}
	}
	if !h.allowAttempt("new-source") {
		t.Fatal("expired enrollment budget did not recover")
	}
}

func TestMaintenanceRetentionAndReceiptBudget(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.ConsumeSignature("device", []byte("old"), time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`INSERT INTO bridge_audit VALUES('old','workspace','device','person','session.get','ok','2000-01-01')`); err != nil {
		t.Fatal(err)
	}
	if err = s.Maintain(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"request_receipts", "bridge_audit"} {
		var count int
		if err = s.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("retention %s: %v", table, err)
		}
	}
	if _, err = s.db.Exec(`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<2048) INSERT INTO request_receipts SELECT 'device',CAST(x AS TEXT),'2099-01-01' FROM n`); err != nil {
		t.Fatal(err)
	}
	if err = s.ConsumeSignature("device", []byte("new"), time.Now().Add(time.Minute)); err == nil {
		t.Fatal("unbounded receipts")
	}
	if err = s.ConsumeSignature("other", []byte("new"), time.Now().Add(time.Minute)); err != nil {
		t.Fatal("one device blocked another")
	}
	s.Close()
	if err = s.Maintain(context.Background()); err == nil {
		t.Fatal("closed storage healthy")
	}
	if err = s.ConsumeSignature("device", []byte("x"), time.Now()); err == nil {
		t.Fatal("closed storage accepted signature")
	}
	if err = s.verifyEncryptionKey(); err == nil {
		t.Fatal("closed storage verified key")
	}
}

func TestBackupRetainsSevenAndRejectsUnsafeDestinations(t *testing.T) {
	root := t.TempDir()
	s, err := Open(filepath.Join(root, "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	directory := filepath.Join(root, "backups")
	for i := 0; i < 8; i++ {
		if _, err = s.Backup(context.Background(), directory); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 7 {
		t.Fatalf("retention=%d err=%v", len(entries), err)
	}
	link := filepath.Join(root, "link")
	if err = os.Symlink(directory, link); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Backup(context.Background(), link); err == nil {
		t.Fatal("backup followed symlink")
	}
	file := filepath.Join(root, "file")
	if err = os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Backup(context.Background(), file); err == nil {
		t.Fatal("backup accepted file directory")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.Backup(ctx, directory); err == nil {
		t.Fatal("cancelled backup succeeded")
	}
	if _, err = loadOrCreateBridgeKey(link); err == nil {
		t.Fatal("key followed symlink")
	}
	if _, err = loadOrCreateBridgeKey(filepath.Join(file, "key")); err == nil {
		t.Fatal("key parent file accepted")
	}
}

func TestHostedAssetsAndDocumentationHaveNoIdentityData(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	page := hostedPage(invitePage)
	prefix := `src="/invite/assets/`
	_, suffix, found := strings.Cut(page, prefix)
	if !found {
		t.Fatal("invitation assets would require main app authorization")
	}
	asset, _, _ := strings.Cut(suffix, `"`)
	for _, route := range []string{"/invite/assets/" + asset, "/assets/" + asset, "/docs/mcp"} {
		r := httptest.NewRecorder()
		NewHandler(s, nil, "").ServeHTTP(r, httptest.NewRequest(http.MethodGet, route, nil))
		if r.Code != 200 {
			t.Fatalf("route %s = %d", route, r.Code)
		}
	}
	for _, route := range []string{"/assets/nope.js", "/assets/../index.html", "/index.html"} {
		r := httptest.NewRecorder()
		serveAsset(r, httptest.NewRequest(http.MethodGet, route, nil))
		if r.Code != 404 {
			t.Fatalf("unsafe asset %s = %d", route, r.Code)
		}
	}
	policy := hostedCSP()
	if strings.Contains(policy, "script-src 'unsafe-inline'") || !strings.Contains(policy, "'sha256-") || !strings.Contains(policy, "object-src 'none'") {
		t.Fatal("unsafe CSP")
	}
}

func TestHostedInlineScriptsParseAndMatchCSP(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is required to validate hosted JavaScript syntax")
	}
	policy := hostedCSP()
	for name, page := range map[string]string{"connect": hostedPage(connectPage), "invite": hostedPage(invitePage)} {
		rest := page
		inlineCount := 0
		for {
			_, afterOpen, found := strings.Cut(rest, "<script")
			if !found {
				break
			}
			attributes, body, found := strings.Cut(afterOpen, ">")
			if !found {
				t.Fatalf("%s page has an unterminated script tag", name)
			}
			script, afterClose, found := strings.Cut(body, "</script>")
			if !found {
				t.Fatalf("%s page has an unterminated script body", name)
			}
			rest = afterClose
			if strings.Contains(attributes, "src=") {
				continue
			}
			inlineCount++
			command := exec.Command("node", "--check")
			command.Stdin = strings.NewReader(script)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("%s inline script is invalid: %v\n%s", name, err, output)
			}
			digest := sha256.Sum256([]byte(script))
			hash := "'sha256-" + base64.StdEncoding.EncodeToString(digest[:]) + "'"
			if !strings.Contains(policy, hash) {
				t.Fatalf("%s inline script hash is missing from CSP", name)
			}
		}
		if inlineCount == 0 {
			t.Fatalf("%s page has no inline bootstrap script", name)
		}
	}
}

func TestRegistrationRequiresPrivateKeyPossession(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	public, _, _ := ed25519.GenerateKey(rand.Reader)
	_ = public
	response := httptest.NewRecorder()
	NewHandler(s, nil, "").ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/agent/v1/register", strings.NewReader(`{"name":"Impersonated","public_key":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}`)))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing proof=%d", response.Code)
	}
}

func waitCleanupFixture(t *testing.T) (*Store, Device, Task, Task, ed25519.PrivateKey) {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "synthetic-bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	d := testOwnedDevice(t, s, "synthetic@example.invalid", "Synthetic Cancellation", public)
	if _, err := s.RegisterGrant(d.ID, "synthetic@example.invalid", []protocol.Operation{protocol.SessionList}, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	submit := func(id string) Task {
		task, err := s.Submit("synthetic@example.invalid", d.ID, protocol.Request{Version: 1, RequestID: id, Operation: protocol.SessionList, Arguments: json.RawMessage(`{}`)})
		if err != nil {
			t.Fatal(err)
		}
		return task
	}
	return s, d, submit("cancel-target"), submit("unrelated-control"), private
}

func waitCleanupSnapshot(t *testing.T, s *Store, other Task) string {
	t.Helper()
	// Snapshot every column, including exact encrypted task and public key bytes;
	// synthetic row values stay local and are never printed on failure.
	var snapshot []json.RawMessage
	for _, query := range []string{
		`SELECT * FROM tasks WHERE id=?`,
		`SELECT * FROM devices ORDER BY id`,
		`SELECT * FROM grants ORDER BY device_id,principal`,
		`SELECT * FROM bridge_audit ORDER BY id`,
	} {
		var args []any
		if strings.Contains(query, "WHERE id=?") {
			args = []any{other.ID}
		}
		rows, err := s.db.Query(query, args...)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			t.Fatal(err)
		}
		for rows.Next() {
			values := make([]any, len(columns))
			targets := make([]any, len(columns))
			for i := range values {
				targets[i] = &values[i]
			}
			if err := rows.Scan(targets...); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			encoded, err := json.Marshal(values)
			if err != nil {
				rows.Close()
				t.Fatal(err)
			}
			snapshot = append(snapshot, encoded)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func waitCleanupAssert(t *testing.T, s *Store, d Device, target, other Task, before string) {
	t.Helper()
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM tasks WHERE id=?`, target.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("canceled task retained: count=%d", count)
	}
	if got := waitCleanupSnapshot(t, s, other); got != before {
		t.Error("unrelated task, device, grant, or audit changed")
	}
	next, found, err := s.Next(d.ID)
	if err != nil || !found || next.ID != other.ID {
		t.Fatalf("claim = %q, %v, %v; want unrelated task", next.ID, found, err)
	}
	if _, found, err := s.Next(d.ID); err != nil || found {
		t.Fatalf("canceled task remains claimable: found=%v err=%v", found, err)
	}
}

func awaitWaitPoll(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		buffer := make([]byte, 64<<10)
		for _, block := range strings.Split(string(buffer[:runtime.Stack(buffer, true)]), "\n\n") {
			lines := strings.Split(block, "\n")
			if len(lines) > 1 && strings.Contains(lines[0], "[select]") && strings.Contains(lines[1], "bridge.(*Store).Wait(") {
				return
			}
		}
		runtime.Gosched()
	}
	t.Fatal("Wait did not enter its polling select")
}

func runWaitCancellationCleanup(t *testing.T) {
	for _, mode := range []string{"already-canceled", "already-expired", "query-canceled", "query-deadline", "poll-canceled", "poll-deadline"} {
		t.Run(mode, func(t *testing.T) {
			s, d, target, other, _ := waitCleanupFixture(t)
			before := waitCleanupSnapshot(t, s, other)
			ctx, cancel := context.WithCancel(context.Background())
			want := error(context.Canceled)
			if strings.Contains(mode, "expired") || strings.Contains(mode, "deadline") {
				cancel()
				duration := 300 * time.Millisecond
				if mode == "already-expired" {
					duration = -time.Second
				}
				ctx, cancel = context.WithTimeout(context.Background(), duration)
				want = context.DeadlineExceeded
			}
			defer cancel()
			if mode == "already-canceled" {
				cancel()
			}
			var release func()
			if strings.HasPrefix(mode, "query-") {
				s.db.SetMaxOpenConns(1)
				conn, err := s.db.Conn(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				release = func() { _ = conn.Close() }
				defer release()
			}
			prior := s.db.Stats().WaitCount
			done := make(chan error, 1)
			go func() { _, err := s.Wait(ctx, target.ID, 2*time.Second); done <- err }()
			if release != nil {
				limit := time.Now().Add(time.Second)
				for s.db.Stats().WaitCount == prior && time.Now().Before(limit) {
					runtime.Gosched()
				}
				if s.db.Stats().WaitCount == prior {
					t.Fatal("Wait did not wait for SQL connection")
				}
			} else if strings.HasPrefix(mode, "poll-") {
				awaitWaitPoll(t)
			}
			if strings.HasSuffix(mode, "canceled") {
				cancel()
			}
			if release != nil {
				<-ctx.Done()
				release()
			}
			select {
			case err := <-done:
				if !errors.Is(err, want) {
					t.Fatalf("Wait error=%v, want %v", err, want)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Wait did not return after cancellation")
			}
			waitCleanupAssert(t, s, d, target, other, before)
		})
	}
	t.Run("healthy-completion", func(t *testing.T) {
		s, d, target, other, private := waitCleanupFixture(t)
		claimed, found, err := s.Next(d.ID)
		if err != nil || !found || claimed.ID != target.ID {
			t.Fatalf("claim: %v %v", found, err)
		}
		response := protocol.Response{Version: 1, RequestID: target.Request.RequestID, Status: protocol.StatusOK, Data: map[string]any{"synthetic": true}}
		encoded, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Complete(d.ID, target.ID, encoded, ed25519.Sign(private, encoded)); err != nil {
			t.Fatal(err)
		}
		before := waitCleanupSnapshot(t, s, other)
		got, err := s.Wait(context.Background(), target.ID, time.Second)
		if err != nil || !reflect.DeepEqual(got, response) {
			t.Fatalf("healthy response=%+v err=%v", got, err)
		}
		waitCleanupAssert(t, s, d, target, other, before)
	})
	t.Run("unrelated-active-waiter", func(t *testing.T) {
		s, d, target, other, private := waitCleanupFixture(t)
		before := waitCleanupSnapshot(t, s, other)
		type result struct {
			response protocol.Response
			err      error
		}
		done := make(chan result, 1)
		go func() {
			response, err := s.Wait(context.Background(), other.ID, 2*time.Second)
			done <- result{response, err}
		}()
		awaitWaitPoll(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := s.Wait(ctx, target.ID, time.Second); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if waitCleanupSnapshot(t, s, other) != before {
			t.Fatal("canceled Wait changed healthy waiter's task or authorization")
		}
		claimed, found, err := s.Next(d.ID)
		if err != nil || !found || claimed.ID != other.ID {
			t.Fatalf("healthy claim found=%v err=%v", found, err)
		}
		response := protocol.Response{Version: 1, RequestID: other.Request.RequestID, Status: protocol.StatusOK, Data: map[string]any{"synthetic": true}}
		encoded, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Complete(d.ID, other.ID, encoded, ed25519.Sign(private, encoded)); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-done:
			if got.err != nil || !reflect.DeepEqual(got.response, response) {
				t.Fatalf("unrelated healthy Wait error=%v", got.err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("healthy Wait did not complete")
		}
		if _, found, err := s.Next(d.ID); err != nil || found {
			t.Fatalf("canceled task claimable: found=%v err=%v", found, err)
		}
	})
	t.Run("cleanup-storage-unavailable-is-bounded", func(t *testing.T) {
		s, _, target, other, _ := waitCleanupFixture(t)
		before := waitCleanupSnapshot(t, s, other)
		s.db.SetMaxOpenConns(1)
		conn, err := s.db.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		start := time.Now()
		_, err = s.Wait(ctx, target.ID, time.Second)
		elapsed := time.Since(start)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("original cancellation lost: %v", err)
		}
		if elapsed < 800*time.Millisecond || elapsed > 2*time.Second {
			t.Fatalf("cleanup bound duration=%s", elapsed)
		}
		_ = conn.Close()
		var count int
		if err := s.db.QueryRow(`SELECT count(*) FROM tasks WHERE id=?`, target.ID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("unavailable cleanup should retain task: count=%d err=%v", count, err)
		}
		if waitCleanupSnapshot(t, s, other) != before {
			t.Error("unavailable cleanup changed unrelated state")
		}
	})
	t.Run("ordinary-query-error-preserved", func(t *testing.T) {
		s, _, target, other, _ := waitCleanupFixture(t)
		before := waitCleanupSnapshot(t, s, other)
		path := s.dbPath
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		_, err := s.Wait(context.Background(), target.ID, time.Second)
		if err == nil || !strings.Contains(err.Error(), "database is closed") || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("ordinary SQL error changed: %v", err)
		}
		reopened, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer reopened.Close()
		var count int
		if err := reopened.db.QueryRow(`SELECT count(*) FROM tasks WHERE id=?`, target.ID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("ordinary error removed task: count=%d err=%v", count, err)
		}
		if waitCleanupSnapshot(t, reopened, other) != before {
			t.Error("ordinary error changed unrelated state")
		}
	})
}
