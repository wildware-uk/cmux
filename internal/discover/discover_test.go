package discover

import (
	"context"
	"errors"
	"testing"

	"github.com/wildware-uk/cmux/internal/tmux"
)

type fakeProcs struct {
	procs []Process
	err   error
	calls int
}

func (f *fakeProcs) List(ctx context.Context) ([]Process, error) {
	f.calls++
	return f.procs, f.err
}

func finder(panes []tmux.Pane, procs []Process, current string) (*Finder, *fakeProcs) {
	fp := &fakeProcs{procs: procs}
	return &Finder{
		Client:      &tmux.Fake{Panes: panes},
		Procs:       fp,
		CurrentPane: current,
	}, fp
}

func TestFastPathMatchesPaneCommand(t *testing.T) {
	f, procs := finder([]tmux.Pane{{ID: "%1", PID: 10, Command: "claude"}}, nil, "%1")
	got, err := f.Find(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d Claude panes, want 1", len(got))
	}
	if procs.calls != 0 {
		t.Errorf("fast path should not read the process table, but it did")
	}
}

func TestProcessWalkFindsClaudeUnderWrapper(t *testing.T) {
	// Pane runs zsh; zsh spawned a wrapper; the wrapper spawned claude.
	f, _ := finder(
		[]tmux.Pane{{ID: "%1", PID: 10, Command: "zsh"}},
		[]Process{
			{PID: 11, PPID: 10, Name: "wrapper"},
			{PID: 12, PPID: 11, Name: "claude"},
		},
		"%1",
	)
	got, err := f.Find(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d Claude panes, want 1", len(got))
	}
}

func TestNoMatch(t *testing.T) {
	f, _ := finder(
		[]tmux.Pane{{ID: "%1", PID: 10, Command: "zsh"}},
		[]Process{{PID: 11, PPID: 10, Name: "vim"}},
		"%1",
	)
	got, err := f.Find(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d Claude panes, want 0", len(got))
	}
}

func TestProcessTableSweptOnlyOnce(t *testing.T) {
	f, procs := finder(
		[]tmux.Pane{
			{ID: "%1", PID: 10, Command: "zsh"},
			{ID: "%2", PID: 20, Command: "zsh"},
			{ID: "%3", PID: 30, Command: "zsh"},
		},
		[]Process{{PID: 11, PPID: 10, Name: "vim"}},
		"%1",
	)
	if _, err := f.Find(t.Context()); err != nil {
		t.Fatal(err)
	}
	if procs.calls != 1 {
		t.Errorf("process table read %d times, want 1", procs.calls)
	}
}

func TestProcessTableFailureDegradesToFastPath(t *testing.T) {
	fp := &fakeProcs{err: errors.New("ps exploded")}
	f := &Finder{
		Client: &tmux.Fake{Panes: []tmux.Pane{
			{ID: "%1", PID: 10, Command: "claude"},
			{ID: "%2", PID: 20, Command: "zsh"},
		}},
		Procs:       fp,
		CurrentPane: "%1",
	}
	got, err := f.Find(t.Context())
	if err != nil {
		t.Fatalf("a broken process table should not fail the command: %v", err)
	}
	if len(got) != 1 || got[0].ID != "%1" {
		t.Errorf("got %v, want just the fast-path match", got)
	}
}

func TestResolveDefaultsToCurrentPane(t *testing.T) {
	f, _ := finder([]tmux.Pane{
		{ID: "%1", PID: 10, Command: "claude"},
		{ID: "%2", PID: 20, Command: "claude"},
	}, nil, "%2")
	p, err := f.Resolve(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "%2" || !p.Current {
		t.Errorf("got %+v, want current pane %%2", p)
	}
}

func TestResolveOutsideTmux(t *testing.T) {
	f, _ := finder([]tmux.Pane{{ID: "%1", PID: 10, Command: "claude"}}, nil, "")
	_, err := f.Resolve(t.Context(), "")
	var want NotInTmuxError
	if !errors.As(err, &want) {
		t.Fatalf("got %v, want NotInTmuxError", err)
	}
}

func TestResolveCurrentPaneGone(t *testing.T) {
	f, _ := finder([]tmux.Pane{{ID: "%1", PID: 10, Command: "claude"}}, nil, "%99")
	if _, err := f.Resolve(t.Context(), ""); err == nil {
		t.Fatal("want error when $TMUX_PANE is no longer a real pane")
	}
}

func TestResolveByPaneID(t *testing.T) {
	f, _ := finder([]tmux.Pane{
		{ID: "%1", PID: 10, Command: "claude"},
		{ID: "%12", PID: 20, Command: "zsh"},
	}, nil, "%1")
	p, err := f.Resolve(t.Context(), "%12")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "%12" || p.IsClaude {
		t.Errorf("got %+v, want non-Claude pane %%12", p)
	}
}

func TestResolveByLocation(t *testing.T) {
	f, _ := finder([]tmux.Pane{
		{ID: "%1", PID: 10, Command: "claude", Session: "work", Window: "2", Index: "1"},
	}, nil, "%1")
	p, err := f.Resolve(t.Context(), "work:2.1")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "%1" {
		t.Errorf("got %q, want %%1", p.ID)
	}
}

func TestResolveByIndexUsesClaudePanesOnly(t *testing.T) {
	f, _ := finder([]tmux.Pane{
		{ID: "%1", PID: 10, Command: "zsh"},
		{ID: "%2", PID: 20, Command: "claude"},
		{ID: "%3", PID: 30, Command: "claude"},
	}, nil, "%2")
	p, err := f.Resolve(t.Context(), "1")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "%3" {
		t.Errorf("index 1 resolved to %q, want %%3", p.ID)
	}
}

func TestResolveIndexOutOfRange(t *testing.T) {
	f, _ := finder([]tmux.Pane{{ID: "%1", PID: 10, Command: "claude"}}, nil, "%1")
	if _, err := f.Resolve(t.Context(), "7"); err == nil {
		t.Fatal("want error for an index past the end of the listing")
	}
}

func TestResolveUnknownTarget(t *testing.T) {
	f, _ := finder([]tmux.Pane{{ID: "%1", PID: 10, Command: "claude"}}, nil, "%1")
	if _, err := f.Resolve(t.Context(), "%99"); err == nil {
		t.Fatal("want error for a pane id that does not exist")
	}
}

func TestParsePSSkipsJunk(t *testing.T) {
	procs := ParsePS("  1     0 systemd\nnot a row\n 12    1 /usr/bin/claude\n")
	if len(procs) != 2 {
		t.Fatalf("got %d rows, want 2", len(procs))
	}
	if procs[1].Name != "claude" {
		t.Errorf("path not reduced to base name: %q", procs[1].Name)
	}
}
