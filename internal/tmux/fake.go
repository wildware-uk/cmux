package tmux

import (
	"context"
	"fmt"
	"strings"
)

// Call is one recorded operation against a Fake.
type Call struct {
	Op      string // "list-panes", "send-keys", "load-buffer", "paste-buffer", "run-shell"
	Target  string
	Keys    []string
	Payload string
	Delay   int
	Command string
}

func (c Call) String() string {
	switch c.Op {
	case "send-keys":
		return fmt.Sprintf("send-keys -t %s %s", c.Target, strings.Join(c.Keys, " "))
	case "load-buffer":
		return fmt.Sprintf("load-buffer %q", c.Payload)
	case "paste-buffer":
		return fmt.Sprintf("paste-buffer -t %s", c.Target)
	case "run-shell":
		return fmt.Sprintf("run-shell -b -d %d %q", c.Delay, c.Command)
	default:
		return c.Op
	}
}

// Fake is an in-memory Client that records what it was asked to do.
type Fake struct {
	Panes      []Pane
	Ver        string
	Err        error // returned by every method when set
	Calls      []Call
	LastBuffer string
}

func (f *Fake) ListPanes(ctx context.Context) ([]Pane, error) {
	f.Calls = append(f.Calls, Call{Op: "list-panes"})
	if f.Err != nil {
		return nil, f.Err
	}
	return f.Panes, nil
}

func (f *Fake) SendKeys(ctx context.Context, target string, keys ...string) error {
	f.Calls = append(f.Calls, Call{Op: "send-keys", Target: target, Keys: keys})
	return f.Err
}

func (f *Fake) LoadBuffer(ctx context.Context, payload []byte) error {
	f.LastBuffer = string(payload)
	f.Calls = append(f.Calls, Call{Op: "load-buffer", Payload: string(payload)})
	return f.Err
}

func (f *Fake) PasteBuffer(ctx context.Context, target string) error {
	f.Calls = append(f.Calls, Call{Op: "paste-buffer", Target: target})
	return f.Err
}

func (f *Fake) Version(ctx context.Context) (string, error) {
	if f.Err != nil {
		return "", f.Err
	}
	if f.Ver == "" {
		return "3.4", nil
	}
	return f.Ver, nil
}

func (f *Fake) RunShellDetached(ctx context.Context, delay int, command string) error {
	f.Calls = append(f.Calls, Call{Op: "run-shell", Delay: delay, Command: command})
	return f.Err
}

// Ops renders the recorded calls one per line, for comparing in tests.
func (f *Fake) Ops() []string {
	out := make([]string, 0, len(f.Calls))
	for _, c := range f.Calls {
		out = append(out, c.String())
	}
	return out
}
