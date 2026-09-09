package cli

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden help files")

// golden compares text against testdata/<name>, or rewrites it with -update.
//
// The point is that help text is a real interface — an agent reads it to work
// out what cmux can do — so wording changes should be deliberate, not
// accidental.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run: go test ./internal/cli -update)", err)
	}
	if got != string(want) {
		t.Errorf("%s is out of date.\n--- got ---\n%s\n--- want ---\n%s\n\n"+
			"If the change is intended: go test ./internal/cli -update", name, got, want)
	}
}

func TestRootHelpGolden(t *testing.T) {
	h := runCLI(t, "--help")
	if h.code != 0 {
		t.Fatalf("exit %d", h.code)
	}
	golden(t, "help-root.txt", h.out())
}

func TestCommandHelpGolden(t *testing.T) {
	for _, c := range commands() {
		h := runCLI(t, c.Name, "--help")
		if h.code != 0 {
			t.Fatalf("%s --help: exit %d, stderr %q", c.Name, h.code, h.errs())
		}
		golden(t, "help-"+c.Name+".txt", h.out())
	}
}

func TestHelpListsEveryCommand(t *testing.T) {
	h := runCLI(t, "--help")
	for _, c := range commands() {
		if !strings.Contains(h.out(), c.Name) {
			t.Errorf("root help does not mention %q", c.Name)
		}
	}
}

func TestEveryCommandHasSummaryAndExample(t *testing.T) {
	for _, c := range commands() {
		if c.Summary == "" {
			t.Errorf("%s has no summary", c.Name)
		}
		if example(c) == "" {
			t.Errorf("%s has no example", c.Name)
		}
	}
}

func TestNoArgsPrintsHelpAndFails(t *testing.T) {
	h := runCLI(t)
	if h.code != 2 {
		t.Errorf("exit %d, want 2", h.code)
	}
	if !strings.Contains(h.errs(), "Usage:") {
		t.Errorf("bare cmux should print usage, got %q", h.errs())
	}
}

func TestHelpMentionsTheTimingCaveat(t *testing.T) {
	// The single most surprising thing about cmux; it must not quietly vanish
	// from the help text.
	h := runCLI(t, "--help")
	if !strings.Contains(h.out(), "immediately") {
		t.Error("root help no longer warns that slash commands fire immediately")
	}
}
