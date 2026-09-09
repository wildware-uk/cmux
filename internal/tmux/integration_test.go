//go:build integration

// These tests drive a real tmux server, so they are behind a build tag and kept
// out of the default `go test ./...` run:
//
//	go test -tags integration ./internal/tmux/
//
// The server runs on its own socket and its own session name, so nothing here
// can touch the tmux session you are sitting in.
package tmux

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func server(t *testing.T) Exec {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed; skipping integration tests")
	}
	e := Exec{Socket: fmt.Sprintf("cmux-test-%d", os.Getpid())}
	// A previous run that crashed may have left this behind.
	_ = e.KillServer(context.Background())

	// cat echoes back whatever is typed into it, which is all we need to prove
	// the keystrokes arrived.
	if err := e.NewSession(context.Background(), "t", "cat"); err != nil {
		t.Fatalf("starting test server: %v", err)
	}
	t.Cleanup(func() { _ = e.KillServer(context.Background()) })
	settle()
	return e
}

// settle gives tmux a moment to draw. Generous, because a flaky integration
// test is worse than a slow one.
func settle() { time.Sleep(300 * time.Millisecond) }

func target(t *testing.T, e Exec) string {
	t.Helper()
	panes, err := e.ListPanes(context.Background())
	if err != nil {
		t.Fatalf("ListPanes: %v", err)
	}
	if len(panes) != 1 {
		t.Fatalf("got %d panes, want 1", len(panes))
	}
	return panes[0].ID
}

func send(t *testing.T, e Exec, id, text string) {
	t.Helper()
	ctx := context.Background()
	if err := e.LoadBuffer(ctx, []byte(text)); err != nil {
		t.Fatalf("LoadBuffer: %v", err)
	}
	if err := e.PasteBuffer(ctx, id); err != nil {
		t.Fatalf("PasteBuffer: %v", err)
	}
	if err := e.SendKeys(ctx, id, "Enter"); err != nil {
		t.Fatalf("SendKeys: %v", err)
	}
	settle()
}

func TestTextArrivesIntact(t *testing.T) {
	e := server(t)
	id := target(t, e)
	send(t, e, id, "hello from cmux")

	got, err := e.CapturePane(context.Background(), id)
	if err != nil {
		t.Fatalf("CapturePane: %v", err)
	}
	// cat echoes the line, so it should appear twice: as typed and as echoed.
	if strings.Count(got, "hello from cmux") < 2 {
		t.Errorf("text did not round-trip through the pane:\n%s", got)
	}
}

func TestSpecialCharactersSurvive(t *testing.T) {
	e := server(t)
	id := target(t, e)
	// No shell is involved, so none of this should be expanded or mangled.
	payload := `quotes "x" 'y' unicode émoji 🚀 dollar $HOME backtick ` + "`x`"
	send(t, e, id, payload)

	got, err := e.CapturePane(context.Background(), id)
	if err != nil {
		t.Fatalf("CapturePane: %v", err)
	}
	if !strings.Contains(got, payload) {
		t.Errorf("payload was altered in transit.\nsent: %s\ngot:\n%s", payload, got)
	}
	if strings.Contains(got, os.Getenv("HOME")) {
		t.Error("$HOME was expanded somewhere it should not have been")
	}
}

func TestEnterIsDelivered(t *testing.T) {
	e := server(t)
	id := target(t, e)

	// Without an Enter, cat has nothing to echo, so the text appears once.
	ctx := context.Background()
	if err := e.LoadBuffer(ctx, []byte("no newline yet")); err != nil {
		t.Fatal(err)
	}
	if err := e.PasteBuffer(ctx, id); err != nil {
		t.Fatal(err)
	}
	settle()
	before, err := e.CapturePane(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(before, "no newline yet"); n != 1 {
		t.Fatalf("before Enter: found the text %d times, want 1:\n%s", n, before)
	}

	if err := e.SendKeys(ctx, id, "Enter"); err != nil {
		t.Fatal(err)
	}
	settle()
	after, err := e.CapturePane(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(after, "no newline yet"); n < 2 {
		t.Errorf("after Enter: found the text %d times, want at least 2:\n%s", n, after)
	}
}

func TestListPanesAgainstRealServer(t *testing.T) {
	e := server(t)
	panes, err := e.ListPanes(context.Background())
	if err != nil {
		t.Fatalf("ListPanes: %v", err)
	}
	p := panes[0]
	if !strings.HasPrefix(p.ID, "%") {
		t.Errorf("pane id %q does not look like a tmux pane id", p.ID)
	}
	if p.PID == 0 {
		t.Error("pane pid was not parsed")
	}
	if p.Session != "t" {
		t.Errorf("session = %q, want %q", p.Session, "t")
	}
	if p.Command == "" {
		t.Error("pane_current_command came back empty")
	}
}

func TestVersionAgainstRealServer(t *testing.T) {
	e := server(t)
	v, err := e.Version(context.Background())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v == "" || strings.HasPrefix(v, "tmux ") {
		t.Errorf("Version() = %q, want a bare version like 3.4", v)
	}
}
