// Package menu reads Claude Code's interactive menus out of a captured tmux
// pane, so cmux can drive them.
//
// Everything here is a pure function over the pane's rendered lines. That keeps
// the screen-scraping — which is inherently brittle — in one place, testable
// against recorded fixtures, with no tmux involved.
package menu

import (
	"fmt"
	"regexp"
	"strings"
)

// Caret is the cursor Claude Code draws beside the selected row.
const Caret = "❯"

// ErrNotFound reports that a line could not be located in the pane.
type ErrNotFound struct {
	What       string
	Candidates []string
}

func (e ErrNotFound) Error() string {
	if len(e.Candidates) == 0 {
		return fmt.Sprintf("could not find %s on screen", e.What)
	}
	return fmt.Sprintf("could not find %s on screen; saw: %s",
		e.What, strings.Join(e.Candidates, ", "))
}

// ErrAmbiguous reports that a name matched more than one row.
type ErrAmbiguous struct {
	What    string
	Matches []string
}

func (e ErrAmbiguous) Error() string {
	return fmt.Sprintf("%q matches more than one entry: %s",
		e.What, strings.Join(e.Matches, ", "))
}

// Lines splits a captured pane into rows.
func Lines(pane string) []string {
	return strings.Split(strings.ReplaceAll(pane, "\r\n", "\n"), "\n")
}

// Menu returns just the block the open menu occupies: from the heading down to
// the keyboard footer that closes it.
//
// Everything else on a captured pane is a trap. Claude Code echoes each prompt
// as "❯ /mcp", so a whole transcript of past commands sits above the menu
// wearing the same cursor character the menu uses, and searching the raw pane
// for the cursor finds one of those instead of the selected row. The heading is
// taken from its last occurrence, because earlier runs leave older copies of
// the same menu further up the scrollback.
func Menu(lines []string, heading string) []string {
	start := -1
	for i, l := range lines {
		if strings.Contains(l, heading) {
			start = i
		}
	}
	if start < 0 {
		return nil
	}
	for i := start; i < len(lines); i++ {
		if isHint(lines[i]) {
			return lines[start : i+1]
		}
	}
	return lines[start:]
}

// CaretLine returns the index of the row holding the cursor, or -1.
func CaretLine(lines []string) int {
	for i, l := range lines {
		if strings.Contains(l, Caret) {
			return i
		}
	}
	return -1
}

// CaretRow returns the position of the selected entry within the list, or -1.
//
// The position is counted in entries, not screen lines, because that is what
// an arrow key moves by. The two differ whenever the list is broken up — a
// scope heading like "Project MCPs (.mcp.json)" between groups adds a line the
// cursor never lands on, and counting lines would overshoot by one per
// heading.
func CaretRow(lines []string) int {
	for _, r := range Rows(lines) {
		if r.Selected {
			return r.Index
		}
	}
	return -1
}

// numbered matches an action row like "  ❯ 3. Reconnect".
var numbered = regexp.MustCompile(`^\s*` + Caret + `?\s*(\d+)\.\s+(.*?)\s*$`)

// Option is one row of a numbered action menu.
type Option struct {
	Number int
	Label  string
	Line   int
}

// Options reads the numbered actions from a detail menu.
func Options(lines []string) []Option {
	var out []Option
	for i, l := range lines {
		m := numbered.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		var n int
		fmt.Sscanf(m[1], "%d", &n)
		out = append(out, Option{Number: n, Label: m[2], Line: i})
	}
	return out
}

// FindOption returns the numbered action whose label contains want, case
// insensitively.
//
// The number is read from the screen rather than assumed, because it moves: a
// failed MCP server offers "1. Reconnect", a healthy one offers "3. Reconnect"
// behind View tools and Clear authentication. Pressing a hardcoded 1 on a
// healthy server opens View tools and still looks like it worked.
func FindOption(lines []string, want string) (Option, error) {
	opts := Options(lines)
	var hits []Option
	for _, o := range opts {
		if strings.Contains(strings.ToLower(o.Label), strings.ToLower(want)) {
			hits = append(hits, o)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		var labels []string
		for _, o := range opts {
			labels = append(labels, o.Label)
		}
		return Option{}, ErrNotFound{What: fmt.Sprintf("a %q action", want), Candidates: labels}
	default:
		var labels []string
		for _, o := range hits {
			labels = append(labels, o.Label)
		}
		return Option{}, ErrAmbiguous{What: want, Matches: labels}
	}
}

// Row is one selectable line in a list that has no numbers.
type Row struct {
	Name string
	// Index is the row's position in the list, which is what arrow keys count.
	Index int
	// Line is its position on screen, for reporting.
	Line int
	Text string
	// Selected reports whether the cursor is on this row.
	Selected bool
}

// listSep separates a row's name from its status, as in
// "agent-dashboard · ✔ connected · 24 tools".
const listSep = " · "

// hintMarkers appear in the keyboard-help footer, which uses the same " · "
// separator as the rows above it and would otherwise parse as an entry.
var hintMarkers = []string{"↑/↓", "Esc to", "Enter to", "to navigate"}

func isHint(text string) bool {
	for _, m := range hintMarkers {
		if strings.Contains(text, m) {
			return true
		}
	}
	return false
}

// Rows reads the selectable entries of an unnumbered list, taking each row's
// name as the text before the first separator.
//
// Only rows carrying the separator count, which keeps headings like
// "User MCPs (/home/shaun/.claude.json)" out of the list, and the keyboard
// footer is skipped explicitly because it carries the separator too.
func Rows(lines []string) []Row {
	var out []Row
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		selected := strings.HasPrefix(trimmed, Caret)
		text := strings.TrimSpace(strings.TrimPrefix(trimmed, Caret))
		if !strings.Contains(text, listSep) || isHint(text) {
			continue
		}
		name := strings.TrimSpace(strings.SplitN(text, listSep, 2)[0])
		if name == "" {
			continue
		}
		out = append(out, Row{
			Name: name, Index: len(out), Line: i, Text: text, Selected: selected,
		})
	}
	return out
}

// FindRow locates a list entry by exact name.
//
// The match is exact rather than a substring because names nest:
// "agent-dashboard" is a prefix of "agent-dashboard-channel", and a substring
// match would silently act on the wrong server.
func FindRow(lines []string, name string) (Row, error) {
	rows := Rows(lines)
	var hits []Row
	for _, r := range rows {
		if r.Name == name {
			hits = append(hits, r)
		}
	}
	if len(hits) == 1 {
		return hits[0], nil
	}
	if len(hits) > 1 {
		var names []string
		for _, r := range hits {
			names = append(names, r.Name)
		}
		return Row{}, ErrAmbiguous{What: name, Matches: names}
	}
	var names []string
	for _, r := range rows {
		names = append(names, r.Name)
	}
	return Row{}, ErrNotFound{What: fmt.Sprintf("a row named %q", name), Candidates: names}
}

// StepKeys returns the key presses that move the cursor from one row to
// another. Moving nowhere returns no keys.
func StepKeys(from, to int) []string {
	if from < 0 || to < 0 || from == to {
		return nil
	}
	key, n := "Down", to-from
	if to < from {
		key, n = "Up", from-to
	}
	keys := make([]string, 0, n)
	for range n {
		keys = append(keys, key)
	}
	return keys
}

// Contains reports whether any line holds the given text.
func Contains(lines []string, want string) bool {
	for _, l := range lines {
		if strings.Contains(l, want) {
			return true
		}
	}
	return false
}
