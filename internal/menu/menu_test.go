package menu

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// fixture loads a pane capture recorded from a live Claude Code session.
func fixture(t *testing.T, name string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return Lines(string(b))
}

func TestCaretLine(t *testing.T) {
	lines := fixture(t, "mcp-list.txt")
	if got := CaretLine(lines); got != 4 {
		t.Errorf("CaretLine = %d, want 4 (the agent-dashboard row)", got)
	}
}

func TestCaretMissing(t *testing.T) {
	if got := CaretLine([]string{"no cursor here"}); got != -1 {
		t.Errorf("CaretLine = %d, want -1", got)
	}
}

func TestRowsSkipsHeadings(t *testing.T) {
	rows := Rows(fixture(t, "mcp-list.txt"))
	var names []string
	for _, r := range rows {
		names = append(names, r.Name)
	}
	want := []string{"agent-dashboard", "agent-dashboard-channel"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("got %v, want %v", names, want)
	}
}

func TestFindRowExactNotPrefix(t *testing.T) {
	// The trap: "agent-dashboard" is a prefix of "agent-dashboard-channel".
	// A substring match would act on the wrong server and still look right.
	lines := fixture(t, "mcp-list.txt")

	short, err := FindRow(lines, "agent-dashboard")
	if err != nil {
		t.Fatal(err)
	}
	if short.Line != 4 {
		t.Errorf("agent-dashboard resolved to line %d, want 4", short.Line)
	}

	long, err := FindRow(lines, "agent-dashboard-channel")
	if err != nil {
		t.Fatal(err)
	}
	if long.Line != 5 {
		t.Errorf("agent-dashboard-channel resolved to line %d, want 5", long.Line)
	}
	if short.Line == long.Line {
		t.Fatal("the two servers resolved to the same row")
	}
}

func TestFindRowUnknownListsCandidates(t *testing.T) {
	_, err := FindRow(fixture(t, "mcp-list.txt"), "nope")
	var nf ErrNotFound
	if !errors.As(err, &nf) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
	if len(nf.Candidates) != 2 {
		t.Errorf("error should list what was on screen, got %v", nf.Candidates)
	}
}

func TestFindOptionOnFailedServer(t *testing.T) {
	opt, err := FindOption(fixture(t, "mcp-detail-failed.txt"), "Reconnect")
	if err != nil {
		t.Fatal(err)
	}
	if opt.Number != 1 {
		t.Errorf("Reconnect is option %d, want 1 on a failed server", opt.Number)
	}
}

func TestFindOptionOnConnectedServerMovesToThree(t *testing.T) {
	// The whole reason the digit is read rather than hardcoded: pressing 1
	// here opens View tools, and the menu closes either way so it looks fine.
	opt, err := FindOption(fixture(t, "mcp-detail-connected.txt"), "Reconnect")
	if err != nil {
		t.Fatal(err)
	}
	if opt.Number != 3 {
		t.Errorf("Reconnect is option %d, want 3 on a connected server", opt.Number)
	}
	if opt.Label != "Reconnect" {
		t.Errorf("label = %q", opt.Label)
	}
}

func TestOptionsReadsWholeMenu(t *testing.T) {
	opts := Options(fixture(t, "mcp-detail-connected.txt"))
	var labels []string
	for _, o := range opts {
		labels = append(labels, o.Label)
	}
	want := []string{"View tools", "Clear authentication", "Reconnect", "Disable"}
	if !reflect.DeepEqual(labels, want) {
		t.Errorf("got %v, want %v", labels, want)
	}
}

func TestFindOptionMissing(t *testing.T) {
	_, err := FindOption(fixture(t, "mcp-detail-failed.txt"), "View tools")
	var nf ErrNotFound
	if !errors.As(err, &nf) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
	if len(nf.Candidates) != 2 {
		t.Errorf("should report the options it did see, got %v", nf.Candidates)
	}
}

func TestStepKeys(t *testing.T) {
	for _, tc := range []struct {
		from, to int
		want     []string
	}{
		{4, 5, []string{"Down"}},
		{4, 7, []string{"Down", "Down", "Down"}},
		{7, 5, []string{"Up", "Up"}},
		{4, 4, nil},
		{-1, 3, nil},
	} {
		if got := StepKeys(tc.from, tc.to); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("StepKeys(%d, %d) = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestStepFromCaretToRow(t *testing.T) {
	lines := fixture(t, "mcp-list.txt")
	row, err := FindRow(lines, "agent-dashboard-channel")
	if err != nil {
		t.Fatal(err)
	}
	keys := StepKeys(CaretLine(lines), row.Line)
	if !reflect.DeepEqual(keys, []string{"Down"}) {
		t.Errorf("got %v, want one Down", keys)
	}
}

func TestContains(t *testing.T) {
	lines := fixture(t, "mcp-list.txt")
	if !Contains(lines, "Manage MCP servers") {
		t.Error("should find the list header")
	}
	if Contains(lines, "not on this screen") {
		t.Error("false positive")
	}
}

// A real pane carries the transcript above the menu, and Claude Code echoes
// every prompt as "❯ /mcp" — the same character the menu cursor uses.
func TestMenuIgnoresPromptEchoesAboveIt(t *testing.T) {
	pane := Lines(`❯ /mcp
  ⎿  Failed to reconnect to agent-dashboard-channel (detail withheld on this connection).

❯ /mcp
  ⎿  Reconnected to agent-dashboard.

   Manage MCP servers
   2 servers

     User MCPs (/home/shaun/.claude.json)
   ❯ agent-dashboard · ✔ connected · 24 tools
     agent-dashboard-channel · ✘ failed

   ※ Run claude --debug to see error logs
   ↑/↓ to navigate · Enter to confirm · Esc to cancel`)

	view := Menu(pane, "Manage MCP servers")
	if got := CaretRow(view); got != 0 {
		t.Fatalf("caret row = %d, want 0 (the transcript echoes are not the cursor)", got)
	}

	row, err := FindRow(view, "agent-dashboard-channel")
	if err != nil {
		t.Fatal(err)
	}
	if keys := StepKeys(CaretRow(view), row.Index); len(keys) != 1 || keys[0] != "Down" {
		t.Fatalf("keys = %v, want one Down", keys)
	}
}

// A heading between scopes is a line the cursor never lands on, so steps are
// counted in entries rather than screen lines.
func TestStepsAreCountedInEntriesNotLines(t *testing.T) {
	view := Lines(`   Manage MCP servers

     User MCPs (/home/shaun/.claude.json)
   ❯ alpha · ✔ connected

     Project MCPs (.mcp.json)
     beta · ✘ failed

   ↑/↓ to navigate · Enter to confirm · Esc to cancel`)

	row, err := FindRow(view, "beta")
	if err != nil {
		t.Fatal(err)
	}
	if keys := StepKeys(CaretRow(view), row.Index); len(keys) != 1 {
		t.Fatalf("keys = %v, want one Down (three screen lines apart)", keys)
	}
}
