package cli

import (
	"strings"
	"testing"

	"github.com/wildware-uk/cmux/internal/discover"
	"github.com/wildware-uk/cmux/internal/tmux"
)

func TestPanesListsClaudePanesWithIndexes(t *testing.T) {
	h := runCLI(t, "panes")
	if h.code != 0 {
		t.Fatalf("exit %d: %s", h.code, h.errs())
	}
	want := strings.Join([]string{
		"#  PANE  LOCATION  COMMAND  ",
		"0  %1    work:0.0  claude     (this pane)",
		"1  %12   work:2.0  claude   ",
		"",
	}, "\n")
	if h.out() != want {
		t.Errorf("got:\n%q\nwant:\n%q", h.out(), want)
	}
}

func TestPanesAllShowsDetectionColumn(t *testing.T) {
	h := runCLI(t, "panes", "--all-panes")
	if h.code != 0 {
		t.Fatalf("exit %d: %s", h.code, h.errs())
	}
	if !strings.Contains(h.out(), "CLAUDE") {
		t.Errorf("missing detection column:\n%s", h.out())
	}
	if !strings.Contains(h.out(), "%99") {
		t.Errorf("--all-panes should include the shell pane:\n%s", h.out())
	}
	if !strings.Contains(h.out(), "zsh") || !strings.Contains(h.out(), "no") {
		t.Errorf("shell pane should be marked not-Claude:\n%s", h.out())
	}
}

func TestPanesEmpty(t *testing.T) {
	h := newHarness("panes")
	h.fake.Panes = nil
	h.env.Finder = &discover.Finder{Client: h.fake, Procs: noProcs{}, CurrentPane: "%1"}
	if code := h.run(); code != 0 {
		t.Fatalf("no panes is a normal answer, got exit %d", code)
	}
	if !strings.Contains(h.out(), "no Claude Code panes found") {
		t.Errorf("got %q", h.out())
	}
}

func TestPanesEmptyAll(t *testing.T) {
	h := newHarness("panes", "--all-panes")
	h.fake.Panes = nil
	h.env.Finder = &discover.Finder{Client: h.fake, Procs: noProcs{}, CurrentPane: "%1"}
	h.run()
	if !strings.Contains(h.out(), "no tmux panes found") {
		t.Errorf("got %q", h.out())
	}
}

func TestPanesOnlyOne(t *testing.T) {
	h := newHarness("panes")
	h.fake.Panes = []tmux.Pane{{ID: "%1", PID: 10, Command: "claude", Session: "s", Window: "0", Index: "0"}}
	h.env.Finder = &discover.Finder{Client: h.fake, Procs: noProcs{}, CurrentPane: "%1"}
	h.run()
	if n := strings.Count(h.out(), "\n"); n != 2 {
		t.Errorf("expected a header and one row, got:\n%s", h.out())
	}
}

func TestStatusInsideTmuxOnClaudePane(t *testing.T) {
	h := runCLI(t, "status")
	if h.code != 0 {
		t.Fatalf("exit %d: %s", h.code, h.errs())
	}
	for _, want := range []string{
		"pane:     %1",
		"detected: claude",
		"target:   %1 (work:0.0) [default: current pane]",
		"tmux:     3.4",
		"cmux:     1.2.3",
	} {
		if !strings.Contains(h.out(), want) {
			t.Errorf("missing %q in:\n%s", want, h.out())
		}
	}
}

func TestStatusOnNonClaudePane(t *testing.T) {
	h := runCLI(t, "--to", "%99", "status")
	if !strings.Contains(h.out(), "not a Claude Code pane") {
		t.Errorf("got:\n%s", h.out())
	}
	if !strings.Contains(h.out(), "from --to %99") {
		t.Errorf("should say where the target came from:\n%s", h.out())
	}
}

func TestStatusOutsideTmux(t *testing.T) {
	h := newHarness("status")
	h.env.CurrentPane = ""
	h.env.Finder = &discover.Finder{Client: h.fake, Procs: noProcs{}, CurrentPane: ""}
	if code := h.run(); code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if !strings.Contains(h.errs(), "not running inside tmux") {
		t.Errorf("got %q", h.errs())
	}
}
