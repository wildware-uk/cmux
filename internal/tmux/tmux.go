// Package tmux is the only place in cmux that shells out to the tmux binary.
//
// Everything above it talks to Client, so the rest of the program can be tested
// without a running tmux server.
package tmux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// ErrNotFound reports that the tmux binary is not on PATH.
var ErrNotFound = errors.New("tmux not found on PATH")

// Pane is one tmux pane.
type Pane struct {
	ID      string // unique pane id, e.g. "%12"
	PID     int    // pid of the process tmux started in the pane
	Command string // pane_current_command
	Title   string // pane_title
	Session string
	Window  string // window index
	Index   string // pane index within the window
	Active  bool
}

// Target is the address tmux uses for this pane.
func (p Pane) Target() string { return p.ID }

// Location is the human-readable "session:window.pane" form.
func (p Pane) Location() string {
	return fmt.Sprintf("%s:%s.%s", p.Session, p.Window, p.Index)
}

// Client is the set of tmux operations cmux needs.
type Client interface {
	ListPanes(ctx context.Context) ([]Pane, error)
	SendKeys(ctx context.Context, target string, keys ...string) error
	LoadBuffer(ctx context.Context, payload []byte) error
	PasteBuffer(ctx context.Context, target string) error
	Version(ctx context.Context) (string, error)
	RunShellDetached(ctx context.Context, delay int, command string) error
}

// fieldSep separates fields in the list-panes format string.
//
// It has to be printable: tmux renders control characters in -F output as
// escape text, so a real 0x1f arrives as the four characters \037 and nothing
// splits. Pane titles can hold almost anything, so this is a string a title is
// very unlikely to contain, and the title is placed last so that even a title
// containing it cannot corrupt an earlier field.
const fieldSep = "|cmux|"

var listFormat = strings.Join([]string{
	"#{pane_id}",
	"#{pane_pid}",
	"#{pane_current_command}",
	"#{session_name}",
	"#{window_index}",
	"#{pane_index}",
	"#{pane_active}",
	"#{pane_title}", // last: a title containing the separator cannot corrupt earlier fields
}, fieldSep)

// Exec is a Client backed by the real tmux binary.
type Exec struct {
	// Socket, when set, is passed as -L. Used by the integration tests so they
	// never touch the developer's own tmux server.
	Socket string
}

func (e Exec) args(rest ...string) []string {
	if e.Socket == "" {
		return rest
	}
	return append([]string{"-L", e.Socket}, rest...)
}

func (e Exec) run(ctx context.Context, stdin []byte, rest ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "tmux", e.args(rest...)...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, ErrNotFound
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("tmux %s: %s", strings.Join(rest, " "), msg)
	}
	return stdout.Bytes(), nil
}

// ListPanes returns every pane on the server.
func (e Exec) ListPanes(ctx context.Context) ([]Pane, error) {
	out, err := e.run(ctx, nil, "list-panes", "-a", "-F", listFormat)
	if err != nil {
		return nil, err
	}
	return ParsePanes(string(out))
}

// ParsePanes turns list-panes output into panes. Exported for tests.
func ParsePanes(out string) ([]Pane, error) {
	var panes []Pane
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		// SplitN keeps a title containing the separator in one piece.
		f := strings.SplitN(line, fieldSep, 8)
		if len(f) < 8 {
			return nil, fmt.Errorf("unexpected list-panes output: %q", line)
		}
		pid, err := strconv.Atoi(f[1])
		if err != nil {
			return nil, fmt.Errorf("unexpected pane pid %q: %w", f[1], err)
		}
		panes = append(panes, Pane{
			ID:      f[0],
			PID:     pid,
			Command: f[2],
			Session: f[3],
			Window:  f[4],
			Index:   f[5],
			Active:  f[6] == "1",
			Title:   f[7],
		})
	}
	return panes, nil
}

// SendKeys sends key names (or literal text via -l) to a pane.
func (e Exec) SendKeys(ctx context.Context, target string, keys ...string) error {
	args := append([]string{"send-keys", "-t", target}, keys...)
	_, err := e.run(ctx, nil, args...)
	return err
}

// LoadBuffer fills tmux's paste buffer from payload.
func (e Exec) LoadBuffer(ctx context.Context, payload []byte) error {
	_, err := e.run(ctx, payload, "load-buffer", "-")
	return err
}

// PasteBuffer pastes the buffer into a pane.
func (e Exec) PasteBuffer(ctx context.Context, target string) error {
	_, err := e.run(ctx, nil, "paste-buffer", "-t", target)
	return err
}

// Version reports the tmux version, e.g. "3.4".
func (e Exec) Version(ctx context.Context) (string, error) {
	out, err := e.run(ctx, nil, "-V")
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(strings.TrimSpace(string(out)), "tmux "), nil
}

// RunShellDetached asks the tmux server to run a shell command after delay
// seconds. cmux uses it for --defer, so a caller can get its own turn out of the
// way before the keys land.
func (e Exec) RunShellDetached(ctx context.Context, delay int, command string) error {
	_, err := e.run(ctx, nil, "run-shell", "-b", "-d", strconv.Itoa(delay), command)
	return err
}

// CapturePane returns the visible contents of a pane. Only used by the
// integration tests, which need to see what actually arrived.
func (e Exec) CapturePane(ctx context.Context, target string) (string, error) {
	out, err := e.run(ctx, nil, "capture-pane", "-p", "-t", target)
	return string(out), err
}

// NewSession starts a detached session running command. Test helper.
func (e Exec) NewSession(ctx context.Context, name, command string) error {
	_, err := e.run(ctx, nil, "new-session", "-d", "-s", name, "-x", "200", "-y", "50", command)
	return err
}

// KillServer stops the tmux server this Exec talks to. Test helper.
func (e Exec) KillServer(ctx context.Context) error {
	_, err := e.run(ctx, nil, "kill-server")
	return err
}
