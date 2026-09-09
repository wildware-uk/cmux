// Package inject turns a cmux command into the exact list of tmux operations
// that makes it happen.
//
// Operations are returned as data rather than performed here, which is what
// makes --dry-run and the unit tests straightforward.
package inject

import (
	"context"
	"fmt"
	"strings"

	"github.com/wildware-uk/cmux/internal/tmux"
)

// Kind is the sort of tmux operation an Op describes.
type Kind int

const (
	LoadBuffer Kind = iota
	PasteBuffer
	SendKeys
)

// Op is one tmux operation.
type Op struct {
	Kind    Kind
	Payload string   // LoadBuffer
	Keys    []string // SendKeys
}

// String renders the op as the tmux command line it stands for, which is what
// --dry-run prints.
func (o Op) String() string {
	switch o.Kind {
	case LoadBuffer:
		return fmt.Sprintf("printf %%s %s | tmux load-buffer -", shellQuote(o.Payload))
	case PasteBuffer:
		return "tmux paste-buffer -t TARGET"
	case SendKeys:
		return "tmux send-keys -t TARGET " + strings.Join(o.Keys, " ")
	}
	return "?"
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Text builds the ops that put text into the target's input box and submit it.
//
// The trailing space closes Claude Code's slash-command menu, so the Enter
// submits what was typed instead of picking a menu entry. The Enter is a
// separate send-keys because a newline inside the pasted payload does not
// submit reliably once the text wraps. See docs/injection.md.
func Text(s string) []Op {
	return []Op{
		{Kind: LoadBuffer, Payload: Sanitise(s) + " "},
		{Kind: PasteBuffer},
		{Kind: SendKeys, Keys: []string{"Enter"}},
	}
}

// Key builds the ops for a raw key press, with no text and no submit.
func Key(name string) []Op {
	return []Op{{Kind: SendKeys, Keys: []string{name}}}
}

// Sanitise collapses whitespace that would break the injection.
//
// A newline in the payload either submits the message early or is inserted
// literally, depending on whether the text wraps. Neither is what the caller
// meant, so every run of whitespace becomes a single space.
func Sanitise(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Run performs the operations against a pane.
func Run(ctx context.Context, c tmux.Client, target string, ops []Op) error {
	for _, op := range ops {
		var err error
		switch op.Kind {
		case LoadBuffer:
			err = c.LoadBuffer(ctx, []byte(op.Payload))
		case PasteBuffer:
			err = c.PasteBuffer(ctx, target)
		case SendKeys:
			err = c.SendKeys(ctx, target, op.Keys...)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
