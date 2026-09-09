package tmux

import (
	"strings"
	"testing"
)

func line(fields ...string) string { return strings.Join(fields, fieldSep) }

func TestParsePanes(t *testing.T) {
	out := strings.Join([]string{
		line("%0", "100", "claude", "work", "0", "0", "1", "~/src"),
		line("%12", "200", "zsh", "work", "2", "1", "0", "a title with spaces"),
	}, "\n") + "\n"

	panes, err := ParsePanes(out)
	if err != nil {
		t.Fatalf("ParsePanes: %v", err)
	}
	if len(panes) != 2 {
		t.Fatalf("got %d panes, want 2", len(panes))
	}
	if panes[0].ID != "%0" || panes[0].PID != 100 || panes[0].Command != "claude" || !panes[0].Active {
		t.Errorf("first pane parsed wrong: %+v", panes[0])
	}
	if panes[1].Title != "a title with spaces" {
		t.Errorf("title with spaces mangled: %q", panes[1].Title)
	}
	if got, want := panes[1].Location(), "work:2.1"; got != want {
		t.Errorf("Location() = %q, want %q", got, want)
	}
}

func TestParsePanesTitleContainingSeparator(t *testing.T) {
	// The title is the last field, so even a separator inside it is harmless.
	out := line("%1", "5", "claude", "s", "0", "0", "0", "odd"+fieldSep+"title") + "\n"
	panes, err := ParsePanes(out)
	if err != nil {
		t.Fatalf("ParsePanes: %v", err)
	}
	if panes[0].Title != "odd"+fieldSep+"title" {
		t.Errorf("title = %q", panes[0].Title)
	}
}

func TestParsePanesBlankLines(t *testing.T) {
	out := "\n" + line("%1", "5", "claude", "s", "0", "0", "0", "t") + "\n\n"
	panes, err := ParsePanes(out)
	if err != nil {
		t.Fatalf("ParsePanes: %v", err)
	}
	if len(panes) != 1 {
		t.Fatalf("got %d panes, want 1", len(panes))
	}
}

func TestParsePanesRejectsShortLine(t *testing.T) {
	if _, err := ParsePanes("%1" + fieldSep + "5\n"); err == nil {
		t.Fatal("want error for truncated line, got nil")
	}
}

func TestParsePanesRejectsBadPID(t *testing.T) {
	out := line("%1", "not-a-pid", "claude", "s", "0", "0", "0", "t") + "\n"
	if _, err := ParsePanes(out); err == nil {
		t.Fatal("want error for non-numeric pid, got nil")
	}
}

func TestExecMissingBinaryReportsErrNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no tmux here
	_, err := Exec{}.Version(t.Context())
	if err != ErrNotFound {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}
