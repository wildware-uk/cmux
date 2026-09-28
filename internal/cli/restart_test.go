package cli

import (
	"strings"
	"testing"
	"time"
)

// restartRun runs restart as the copy tmux starts, which waits inline.
func restartRun(t *testing.T, screens []string, args ...string) *harness {
	t.Helper()
	h := newHarness(append([]string{"--to", "%12", "restart"}, args...)...)
	h.env.Detached = true
	h.env.Sleep = func(time.Duration) {}
	h.fake.Screens = screens
	h.code = h.run()
	return h
}

func repeat(s string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}

func TestPromptSendsPlainText(t *testing.T) {
	h := runCLI(t, "--to", "%12", "prompt", "pick up", "#12")
	if h.code != 0 {
		t.Fatalf("exit %d: %s", h.code, h.errs())
	}
	if h.fake.LastBuffer != "pick up #12 " {
		t.Errorf("buffer = %q, want the text with no slash command in front", h.fake.LastBuffer)
	}
}

func TestRestartOnSelfHandsItselfToTmux(t *testing.T) {
	h := newHarness("restart", "carry on with #12")
	h.env.LogPath = "/tmp/cmux-test.log"
	h.code = h.run()
	if h.code != 0 {
		t.Fatalf("exit %d: %s", h.code, h.errs())
	}
	ops := h.ops()
	if len(ops) != 1 || !strings.HasPrefix(ops[0], "run-shell -b -d 1") {
		t.Fatalf("got %v, want one deferred run-shell and nothing typed", ops)
	}
	for _, want := range []string{"CMUX_DETACHED=1", "--to %1", "restart", "carry on with #12", ">>'/tmp/cmux-test.log' 2>&1"} {
		if !strings.Contains(ops[0], want) {
			t.Errorf("deferred line lacks %q: %s", want, ops[0])
		}
	}
}

func TestRestartWaitsThenClearsThenPrompts(t *testing.T) {
	screens := append([]string{"busy 1s", "busy 2s", "busy 3s"}, repeat("idle", stillFor)...)
	screens = append(screens, "clearing", "fresh")
	h := restartRun(t, screens, "carry on")
	if h.code != 0 {
		t.Fatalf("exit %d: %s", h.code, h.errs())
	}
	want := []string{
		`load-buffer "/clear "`, "paste-buffer -t %12", "send-keys -t %12 Enter",
		`load-buffer "carry on "`, "paste-buffer -t %12", "send-keys -t %12 Enter",
	}
	if got := h.ops(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("ops:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestRestartSendsNothingWhileThePaneKeepsMoving(t *testing.T) {
	h := newHarness("--to", "%12", "restart", "carry on")
	h.env.Detached = true
	h.env.Sleep = func(time.Duration) {}
	n := 0
	h.fake.OnCapture = func(int) { n++; h.fake.Screen = strings.Repeat(".", n%3) }
	h.code = h.run()
	if h.code == 0 {
		t.Fatal("want failure when the turn never ends")
	}
	if ops := h.ops(); len(ops) != 0 {
		t.Errorf("sent %v to a pane that never went still", ops)
	}
}

func TestRestartHoldsThePromptIfClearNeverLands(t *testing.T) {
	h := restartRun(t, repeat("idle", stillFor), "carry on")
	if h.code == 0 {
		t.Fatal("want failure when the screen never changes after /clear")
	}
	if strings.Contains(strings.Join(h.ops(), "\n"), "carry on") {
		t.Errorf("prompt sent into an uncleared session: %v", h.ops())
	}
}

func TestDeferredOutputNeverReachesTheScreen(t *testing.T) {
	h := runCLI(t, "--defer", "5", "clear")
	if ops := h.ops(); len(ops) != 1 || !strings.Contains(ops[0], ">/dev/null 2>&1") {
		t.Fatalf("deferred output must be redirected, or tmux shows it in view mode over the pane: %v", ops)
	}
}
