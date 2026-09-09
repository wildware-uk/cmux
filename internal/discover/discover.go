// Package discover works out which tmux panes are running Claude Code, and
// turns a user-supplied target into one of them.
package discover

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/wildware-uk/cmux/internal/tmux"
)

// ClaudeCommand is the process name cmux looks for.
const ClaudeCommand = "claude"

// Pane is a tmux pane plus what cmux worked out about it.
type Pane struct {
	tmux.Pane
	IsClaude bool
	// Current reports whether this is the pane cmux itself was run from.
	Current bool
}

// Process is one row of the process table.
type Process struct {
	PID  int
	PPID int
	Name string
}

// ProcessLister reads the process table. Swapped out in tests.
type ProcessLister interface {
	List(ctx context.Context) ([]Process, error)
}

// PS reads the process table with ps, which works the same on Linux and macOS.
type PS struct{}

func (PS) List(ctx context.Context) ([]Process, error) {
	out, err := exec.CommandContext(ctx, "ps", "-eo", "pid=,ppid=,comm=").Output()
	if err != nil {
		return nil, fmt.Errorf("reading process table: %w", err)
	}
	return ParsePS(string(out)), nil
}

// ParsePS turns "pid ppid comm" rows into processes. Exported for tests.
func ParsePS(out string) []Process {
	var procs []Process
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 3 {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		ppid, err := strconv.Atoi(f[1])
		if err != nil {
			continue
		}
		// comm can itself contain spaces on some systems; keep the whole tail.
		name := strings.Join(f[2:], " ")
		procs = append(procs, Process{PID: pid, PPID: ppid, Name: baseName(name)})
	}
	return procs
}

func baseName(s string) string {
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		return s[i+1:]
	}
	return s
}

// Finder answers questions about panes. Its process-table sweep is done at most
// once per Finder, not once per pane.
type Finder struct {
	Client tmux.Client
	Procs  ProcessLister
	// CurrentPane is the pane cmux is running in, normally $TMUX_PANE.
	CurrentPane string

	children map[int][]Process
	swept    bool
}

// New builds a Finder for the real world.
func New(c tmux.Client) *Finder {
	return &Finder{Client: c, Procs: PS{}, CurrentPane: os.Getenv("TMUX_PANE")}
}

// All returns every pane, each marked with whether it looks like Claude Code.
func (f *Finder) All(ctx context.Context) ([]Pane, error) {
	raw, err := f.Client.ListPanes(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Pane, 0, len(raw))
	for _, p := range raw {
		out = append(out, Pane{
			Pane:     p,
			IsClaude: f.isClaude(ctx, p),
			Current:  p.ID == f.CurrentPane,
		})
	}
	return out, nil
}

// Find returns only the panes running Claude Code.
func (f *Finder) Find(ctx context.Context) ([]Pane, error) {
	all, err := f.All(ctx)
	if err != nil {
		return nil, err
	}
	var out []Pane
	for _, p := range all {
		if p.IsClaude {
			out = append(out, p)
		}
	}
	return out, nil
}

// isClaude checks the pane's own command first, and only walks the process tree
// if that misses. Claude launched under a shell wrapper shows the wrapper as
// pane_current_command, so the walk is what catches those.
func (f *Finder) isClaude(ctx context.Context, p tmux.Pane) bool {
	if baseName(p.Command) == ClaudeCommand {
		return true
	}
	return f.hasClaudeDescendant(ctx, p.PID)
}

func (f *Finder) hasClaudeDescendant(ctx context.Context, root int) bool {
	f.sweep(ctx)
	seen := map[int]bool{}
	queue := []int{root}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		if seen[pid] {
			continue // cycles should not happen, but a bad ps row should not hang us
		}
		seen[pid] = true
		for _, child := range f.children[pid] {
			if child.Name == ClaudeCommand {
				return true
			}
			queue = append(queue, child.PID)
		}
	}
	return false
}

func (f *Finder) sweep(ctx context.Context) {
	if f.swept {
		return
	}
	f.swept = true
	f.children = map[int][]Process{}
	procs, err := f.Procs.List(ctx)
	if err != nil {
		return // detection degrades to the fast path; not worth failing the command
	}
	for _, p := range procs {
		f.children[p.PPID] = append(f.children[p.PPID], p)
	}
}

// NotInTmuxError is returned when there is no target and no way to guess one.
type NotInTmuxError struct{}

func (NotInTmuxError) Error() string {
	return "not running inside tmux; pass --to to pick a pane"
}

// NotClaudeError is returned when a resolved pane is not running Claude Code.
type NotClaudeError struct{ Pane Pane }

func (e NotClaudeError) Error() string {
	return fmt.Sprintf("%s does not look like a Claude Code pane; pass --all-panes to send anyway", e.Pane.ID)
}

// Resolve turns a target string into a pane.
//
// It accepts a pane id ("%12"), a tmux target ("session:window.pane"), an index
// from the `cmux panes` listing, or "" meaning the current pane.
func (f *Finder) Resolve(ctx context.Context, spec string) (Pane, error) {
	all, err := f.All(ctx)
	if err != nil {
		return Pane{}, err
	}

	if spec == "" {
		if f.CurrentPane == "" {
			return Pane{}, NotInTmuxError{}
		}
		for _, p := range all {
			if p.ID == f.CurrentPane {
				return p, nil
			}
		}
		return Pane{}, fmt.Errorf("current pane %s is not in the pane list", f.CurrentPane)
	}

	if strings.HasPrefix(spec, "%") {
		for _, p := range all {
			if p.ID == spec {
				return p, nil
			}
		}
		return Pane{}, unknownTarget(spec)
	}

	// A bare number indexes the `cmux panes` listing, which shows Claude panes.
	if n, err := strconv.Atoi(spec); err == nil {
		claude := filterClaude(all)
		if n < 0 || n >= len(claude) {
			return Pane{}, fmt.Errorf("no pane at index %d; try: cmux panes", n)
		}
		return claude[n], nil
	}

	// Otherwise treat it as session:window.pane, matching as much as was given.
	for _, p := range all {
		if p.Location() == spec {
			return p, nil
		}
	}
	return Pane{}, unknownTarget(spec)
}

func unknownTarget(spec string) error {
	return fmt.Errorf("no pane matching %q; try: cmux panes", spec)
}

func filterClaude(all []Pane) []Pane {
	var out []Pane
	for _, p := range all {
		if p.IsClaude {
			out = append(out, p)
		}
	}
	return out
}
