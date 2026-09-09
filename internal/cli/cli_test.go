package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/wildware-uk/cmux/internal/discover"
	"github.com/wildware-uk/cmux/internal/tmux"
)

// harness builds an Env over a fake tmux server with two Claude panes and one
// shell pane. %1 is the pane cmux is "running in".
type harness struct {
	env    Env
	fake   *tmux.Fake
	stdout *bytes.Buffer
	stderr *bytes.Buffer
	code   int
}

func newHarness(args ...string) *harness {
	fake := &tmux.Fake{Panes: []tmux.Pane{
		{ID: "%1", PID: 10, Command: "claude", Session: "work", Window: "0", Index: "0", Active: true},
		{ID: "%12", PID: 20, Command: "claude", Session: "work", Window: "2", Index: "0"},
		{ID: "%99", PID: 30, Command: "zsh", Session: "work", Window: "3", Index: "0"},
	}}
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	finder := &discover.Finder{Client: fake, Procs: noProcs{}, CurrentPane: "%1"}
	return &harness{
		fake:   fake,
		stdout: stdout,
		stderr: stderr,
		env: Env{
			Args: args, Stdout: stdout, Stderr: stderr,
			Client: fake, Finder: finder,
			Build:       BuildInfo{Version: "1.2.3", Commit: "abc123", Date: "2026-01-01"},
			CurrentPane: "%1",
			Self:        "/usr/bin/cmux",
		},
	}
}

type noProcs struct{}

func (noProcs) List(ctx context.Context) ([]discover.Process, error) { return nil, nil }

func (h *harness) run() int     { return Run(h.env) }
func (h *harness) out() string  { return h.stdout.String() }
func (h *harness) errs() string { return h.stderr.String() }

// ops returns the operations that changed something, dropping the read-only
// list-panes every command makes to resolve its target.
func (h *harness) ops() []string {
	var out []string
	for _, op := range h.fake.Ops() {
		if op != "list-panes" {
			out = append(out, op)
		}
	}
	return out
}

func runCLI(t *testing.T, args ...string) *harness {
	t.Helper()
	h := newHarness(args...)
	h.code = h.run()
	return h
}

func TestSlashCommands(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"compact"}, "/compact "},
		{[]string{"clear"}, "/clear "},
		{[]string{"resume"}, "/resume "},
		{[]string{"context"}, "/context "},
		{[]string{"cost"}, "/cost "},
		{[]string{"agents"}, "/agents "},
		{[]string{"model", "opus"}, "/model opus "},
		{[]string{"goal", "Ship", "the", "parser"}, "/goal Ship the parser "},
	} {
		h := runCLI(t, tc.args...)
		if h.code != 0 {
			t.Fatalf("%v: exit %d, stderr %q", tc.args, h.code, h.errs())
		}
		if h.fake.LastBuffer != tc.want {
			t.Errorf("%v sent %q, want %q", tc.args, h.fake.LastBuffer, tc.want)
		}
		wantOps := []string{
			`load-buffer ` + quote(tc.want),
			"paste-buffer -t %1",
			"send-keys -t %1 Enter",
		}
		if got := strings.Join(h.ops(), "\n"); got != strings.Join(wantOps, "\n") {
			t.Errorf("%v ops:\n%s\nwant:\n%s", tc.args, got, strings.Join(wantOps, "\n"))
		}
	}
}

func quote(s string) string { return "\"" + s + "\"" }

func TestGoalTakesEverythingLiterally(t *testing.T) {
	// A dash-leading word must not be eaten as a flag.
	h := runCLI(t, "goal", "fix", "--to", "be", "decided")
	if h.code != 0 {
		t.Fatalf("exit %d: %s", h.code, h.errs())
	}
	if want := "/goal fix --to be decided "; h.fake.LastBuffer != want {
		t.Errorf("got %q, want %q", h.fake.LastBuffer, want)
	}
}

func TestGoalCollapsesNewlines(t *testing.T) {
	h := runCLI(t, "goal", "line one\nline two")
	if want := "/goal line one line two "; h.fake.LastBuffer != want {
		t.Errorf("got %q, want %q", h.fake.LastBuffer, want)
	}
}

func TestUnknownCommand(t *testing.T) {
	h := runCLI(t, "bogus")
	if h.code != 2 {
		t.Errorf("exit %d, want 2", h.code)
	}
	if !strings.Contains(h.errs(), `unknown command "bogus"`) {
		t.Errorf("stderr = %q", h.errs())
	}
	if len(h.ops()) != 0 {
		t.Errorf("unknown command still touched tmux: %v", h.ops())
	}
}

func TestMissingRequiredArgument(t *testing.T) {
	h := runCLI(t, "goal")
	if h.code != 2 {
		t.Errorf("exit %d, want 2", h.code)
	}
	if !strings.Contains(h.errs(), "goal needs an argument") {
		t.Errorf("stderr = %q", h.errs())
	}
}

func TestArgumentsRejectedWhereNoneTaken(t *testing.T) {
	h := runCLI(t, "compact", "extra")
	if h.code != 2 {
		t.Errorf("exit %d, want 2", h.code)
	}
	if len(h.ops()) != 0 {
		t.Errorf("touched tmux anyway: %v", h.ops())
	}
}

func TestInterruptRefusesSelf(t *testing.T) {
	h := runCLI(t, "interrupt")
	if h.code != 1 {
		t.Errorf("exit %d, want 1", h.code)
	}
	if !strings.Contains(h.errs(), "pass --self") {
		t.Errorf("stderr = %q", h.errs())
	}
	if len(h.ops()) != 0 {
		t.Errorf("sent keys despite refusing: %v", h.ops())
	}
}

func TestInterruptWithSelf(t *testing.T) {
	h := runCLI(t, "--self", "interrupt")
	if h.code != 0 {
		t.Fatalf("exit %d: %s", h.code, h.errs())
	}
	if got, want := strings.Join(h.ops(), ""), "send-keys -t %1 Escape"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInterruptOtherPaneNeedsNoFlag(t *testing.T) {
	h := runCLI(t, "--to", "%12", "interrupt")
	if h.code != 0 {
		t.Fatalf("exit %d: %s", h.code, h.errs())
	}
	if got, want := strings.Join(h.ops(), ""), "send-keys -t %12 Escape"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInterruptSendsNoEnter(t *testing.T) {
	h := runCLI(t, "--to", "%12", "interrupt")
	for _, op := range h.ops() {
		if strings.Contains(op, "Enter") {
			t.Fatalf("interrupt must not submit anything, got %v", h.ops())
		}
	}
}

func TestNonClaudePaneRefused(t *testing.T) {
	h := runCLI(t, "--to", "%99", "compact")
	if h.code != 1 {
		t.Errorf("exit %d, want 1", h.code)
	}
	if !strings.Contains(h.errs(), "--all-panes") {
		t.Errorf("stderr = %q", h.errs())
	}
	if len(h.ops()) != 0 {
		t.Errorf("sent to a non-Claude pane: %v", h.ops())
	}
}

func TestAllPanesOverride(t *testing.T) {
	h := runCLI(t, "--to", "%99", "--all-panes", "compact")
	if h.code != 0 {
		t.Fatalf("exit %d: %s", h.code, h.errs())
	}
	if h.fake.LastBuffer != "/compact " {
		t.Errorf("got %q", h.fake.LastBuffer)
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	h := runCLI(t, "--dry-run", "compact")
	if h.code != 0 {
		t.Fatalf("exit %d: %s", h.code, h.errs())
	}
	// list-panes is a read; nothing else should have happened.
	for _, c := range h.fake.Calls {
		if c.Op != "list-panes" {
			t.Errorf("--dry-run performed %q", c.Op)
		}
	}
	want := "printf %s '/compact ' | tmux load-buffer -\ntmux paste-buffer -t %1\ntmux send-keys -t %1 Enter\n"
	if h.out() != want {
		t.Errorf("got:\n%s\nwant:\n%s", h.out(), want)
	}
}

func TestDestructiveSelfWarns(t *testing.T) {
	h := runCLI(t, "clear")
	if h.code != 0 {
		t.Fatalf("exit %d: %s", h.code, h.errs())
	}
	if !strings.Contains(h.errs(), "warning") || !strings.Contains(h.errs(), "--defer") {
		t.Errorf("expected a warning about firing mid-turn, got %q", h.errs())
	}
	if h.fake.LastBuffer != "/clear " {
		t.Errorf("warning should not stop the send, got %q", h.fake.LastBuffer)
	}
}

func TestDestructiveOtherPaneDoesNotWarn(t *testing.T) {
	h := runCLI(t, "--to", "%12", "clear")
	if strings.Contains(h.errs(), "warning") {
		t.Errorf("no warning expected for another pane, got %q", h.errs())
	}
}

func TestDeferSchedulesInsteadOfSending(t *testing.T) {
	h := runCLI(t, "--defer", "5", "clear")
	if h.code != 0 {
		t.Fatalf("exit %d: %s", h.code, h.errs())
	}
	ops := h.ops()
	if len(ops) != 1 || !strings.HasPrefix(ops[0], "run-shell -b -d 5") {
		t.Fatalf("got %v, want a single deferred run-shell", ops)
	}
	if !strings.Contains(ops[0], "--to %1") || !strings.Contains(ops[0], "clear") {
		t.Errorf("deferred command line looks wrong: %s", ops[0])
	}
	if strings.Contains(ops[0], "--defer") {
		t.Errorf("deferred command must not reschedule itself: %s", ops[0])
	}
}

func TestDeferRejectsNonsense(t *testing.T) {
	h := runCLI(t, "--defer", "soon", "clear")
	if h.code != 2 {
		t.Errorf("exit %d, want 2", h.code)
	}
}

func TestOutsideTmux(t *testing.T) {
	h := newHarness("compact")
	h.env.CurrentPane = ""
	h.env.Finder = &discover.Finder{Client: h.fake, Procs: noProcs{}, CurrentPane: ""}
	if code := h.run(); code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if !strings.Contains(h.errs(), "not running inside tmux") {
		t.Errorf("stderr = %q", h.errs())
	}
}

func TestTmuxMissing(t *testing.T) {
	h := newHarness("compact")
	h.fake.Err = tmux.ErrNotFound
	if code := h.run(); code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if !strings.Contains(h.errs(), "tmux not found on PATH") {
		t.Errorf("stderr = %q", h.errs())
	}
}

func TestUnknownTarget(t *testing.T) {
	h := runCLI(t, "--to", "%404", "compact")
	if h.code != 1 {
		t.Errorf("exit %d, want 1", h.code)
	}
	if !strings.Contains(h.errs(), "cmux panes") {
		t.Errorf("error should point at cmux panes, got %q", h.errs())
	}
}

func TestVersion(t *testing.T) {
	h := runCLI(t, "--version")
	if h.code != 0 || !strings.Contains(h.out(), "1.2.3") {
		t.Errorf("exit %d, out %q", h.code, h.out())
	}
}

func TestSuccessLine(t *testing.T) {
	h := runCLI(t, "--to", "%12", "compact")
	if want := "sent /compact to %12 (work:2.0)\n"; h.out() != want {
		t.Errorf("got %q, want %q", h.out(), want)
	}
}
