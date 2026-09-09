package cli

import (
	"strings"
	"testing"
	"time"
)

// Screens recorded from a live Claude Code pane.
const (
	screenList = `   Manage MCP servers
   2 servers

     User MCPs (/home/shaun/.claude.json)
   ❯ agent-dashboard · ✔ connected · 24 tools
     agent-dashboard-channel · ✘ failed

   ↑/↓ to navigate · Enter to confirm · Esc to cancel`

	screenDetailFailed = `   Agent-dashboard-channel MCP Server

   Status:           ✘ failed

   ❯ 1. Reconnect
     2. Disable

   ↑/↓ to navigate · Enter to select · Esc to back`

	screenDetailConnected = `   Agent-dashboard MCP Server

   Status:           ✔ connected

   ❯ 1. View tools
     2. Clear authentication
     3. Reconnect
     4. Disable

   ↑/↓ to navigate · Enter to select · Esc to back`

	// The prompt, as it looks before any menu is opened.
	screenPrompt = `> ` + "\n" + `  ? for shortcuts`

	screenReconnected = `❯ /mcp
  ⎿  Reconnected to agent-dashboard-channel.`

	screenFailedResult = `❯ /mcp
  ⎿  Failed to reconnect to agent-dashboard-channel (detail withheld on this connection).`
)

// settled repeats each screen, because cmux waits for a menu to hold still for
// two captures before pressing anything.
func settled(screens []string) []string {
	var out []string
	for _, s := range screens {
		out = append(out, s, s)
	}
	return out
}

// mcpRun drives a reconnect against a scripted sequence of screens.
func mcpRun(t *testing.T, screens []string, args ...string) *harness {
	t.Helper()
	h := newHarness(args...)
	// Every run starts by reading the pane it is about to drive.
	h.fake.Screens = append([]string{screenPrompt}, settled(screens)...)
	h.env.Sleep = func(time.Duration) {} // do not actually poll-wait
	h.code = h.run()
	return h
}

func TestMCPReconnectFailedServerUsesDigitOne(t *testing.T) {
	h := mcpRun(t,
		[]string{screenList, screenDetailFailed, screenReconnected},
		"mcp", "reconnect", "agent-dashboard-channel")
	if h.code != 0 {
		t.Fatalf("exit %d: %s", h.code, h.errs())
	}

	ops := strings.Join(h.ops(), "\n")
	for _, want := range []string{
		`load-buffer "/mcp "`,   // open the menu
		"send-keys -t %1 Down",  // step onto the channel row
		"send-keys -t %1 Enter", // open its detail
		"send-keys -t %1 1",     // Reconnect is option 1 when failed
	} {
		if !strings.Contains(ops, want) {
			t.Errorf("missing %q in:\n%s", want, ops)
		}
	}
	if !strings.Contains(h.out(), "reconnected agent-dashboard-channel") {
		t.Errorf("output = %q", h.out())
	}
}

func TestMCPReconnectConnectedServerUsesDigitThree(t *testing.T) {
	// The digit moves with the server's state. Hardcoding 1 here would open
	// View tools and still look like it worked.
	h := mcpRun(t,
		[]string{screenList, screenDetailConnected, screenReconnected},
		"mcp", "reconnect", "agent-dashboard")
	if h.code != 0 {
		t.Fatalf("exit %d: %s", h.code, h.errs())
	}
	ops := strings.Join(h.ops(), "\n")
	if !strings.Contains(ops, "send-keys -t %1 3") {
		t.Errorf("should have sent 3 for Reconnect, got:\n%s", ops)
	}
	if strings.Contains(ops, "send-keys -t %1 1") {
		t.Errorf("sent 1, which is View tools on a connected server:\n%s", ops)
	}
}

func TestMCPReconnectCursorAlreadyOnTargetSendsNoSteps(t *testing.T) {
	// The caret starts on agent-dashboard, so no Down is needed.
	h := mcpRun(t,
		[]string{screenList, screenDetailConnected, screenReconnected},
		"mcp", "reconnect", "agent-dashboard")
	ops := strings.Join(h.ops(), "\n")
	if strings.Contains(ops, "Down") || strings.Contains(ops, "Up") {
		t.Errorf("no cursor movement expected, got:\n%s", ops)
	}
}

func TestMCPReconnectPrefixNameDoesNotMatchLongerServer(t *testing.T) {
	// agent-dashboard must not resolve to agent-dashboard-channel.
	h := mcpRun(t,
		[]string{screenList, screenDetailConnected, screenReconnected},
		"mcp", "reconnect", "agent-dashboard")
	ops := strings.Join(h.ops(), "\n")
	if strings.Contains(ops, "Down") {
		t.Errorf("stepped onto the channel row for the shorter name:\n%s", ops)
	}
}

func TestMCPReconnectUnknownServerListsCandidates(t *testing.T) {
	h := mcpRun(t,
		[]string{screenList, screenDetailFailed, screenReconnected},
		"mcp", "reconnect", "nope")
	if h.code != 1 {
		t.Errorf("exit %d, want 1", h.code)
	}
	if !strings.Contains(h.errs(), "agent-dashboard") {
		t.Errorf("error should list what it saw, got %q", h.errs())
	}
	ops := strings.Join(h.ops(), "\n")
	if strings.Contains(ops, "send-keys -t %1 1") || strings.Contains(ops, "send-keys -t %1 3") {
		t.Errorf("pressed an action digit despite not finding the server:\n%s", ops)
	}
	if !strings.Contains(ops, "Escape") {
		t.Errorf("should back out of the menu it opened:\n%s", ops)
	}
}

func TestMCPReconnectFailureExitsNonZero(t *testing.T) {
	h := mcpRun(t,
		[]string{screenList, screenDetailFailed, screenFailedResult},
		"mcp", "reconnect", "agent-dashboard-channel")
	if h.code != 1 {
		t.Errorf("exit %d, want 1 when the reconnect failed", h.code)
	}
	if !strings.Contains(h.errs(), "failed to reconnect") {
		t.Errorf("stderr = %q", h.errs())
	}
	if strings.Contains(h.out(), "reconnected") {
		t.Errorf("must not claim success: %q", h.out())
	}
}

func TestMCPReconnectDryRunPressesNothing(t *testing.T) {
	h := mcpRun(t,
		[]string{screenList, screenDetailFailed, screenReconnected},
		"--dry-run", "mcp", "reconnect", "agent-dashboard-channel")
	if h.code != 0 {
		t.Fatalf("exit %d: %s", h.code, h.errs())
	}
	for _, op := range h.ops() {
		if strings.HasPrefix(op, "send-keys") || strings.HasPrefix(op, "load-buffer") {
			t.Errorf("--dry-run performed %q", op)
		}
	}
	if !strings.Contains(h.out(), "would open /mcp") {
		t.Errorf("output = %q", h.out())
	}
}

func TestMCPReconnectNeedsServerName(t *testing.T) {
	h := runCLI(t, "mcp", "reconnect")
	if h.code != 2 {
		t.Errorf("exit %d, want 2", h.code)
	}
}

func TestMCPNeedsSubcommand(t *testing.T) {
	h := runCLI(t, "mcp")
	if h.code != 2 {
		t.Errorf("exit %d, want 2", h.code)
	}
	if !strings.Contains(h.errs(), "reconnect") {
		t.Errorf("should name the available subcommands, got %q", h.errs())
	}
}

func TestMCPUnknownSubcommand(t *testing.T) {
	h := runCLI(t, "mcp", "bogus")
	if h.code != 2 {
		t.Errorf("exit %d, want 2", h.code)
	}
}

// A stale line about a similarly named server must not be read as this run's
// result: "agent-dashboard" is a prefix of "agent-dashboard-channel", and a
// previous reconnect leaves its outcome in the transcript.
func TestMCPReconnectIgnoresStaleResultForPrefixName(t *testing.T) {
	stale := `❯ /mcp
  ⎿  Failed to reconnect to agent-dashboard-channel (detail withheld on this connection).`
	done := stale + `
❯ /mcp
  ⎿  Reconnected to agent-dashboard.`

	h := newHarness("mcp", "reconnect", "agent-dashboard")
	// The stale failure is already on screen when the command starts.
	h.fake.Screens = append([]string{stale},
		settled([]string{screenList, screenDetailConnected, done})...)
	h.env.Sleep = func(time.Duration) {}
	h.code = h.run()

	if h.code != 0 {
		t.Fatalf("exit %d: %s", h.code, h.errs())
	}
	if !strings.Contains(h.out(), "reconnected agent-dashboard") {
		t.Fatalf("output = %q", h.out())
	}
}

// A menu left open by an earlier run must be dismissed, or /mcp is typed into
// a dialog instead of the prompt.
func TestMCPReconnectClearsStaleMenuFirst(t *testing.T) {
	h := mcpRun(t,
		[]string{screenList, screenDetailFailed, screenReconnected},
		"mcp", "reconnect", "agent-dashboard-channel")
	if h.code != 0 {
		t.Fatalf("exit %d: %s", h.code, h.errs())
	}
	ops := h.ops()
	if len(ops) == 0 || !strings.Contains(ops[0], "Escape") {
		t.Fatalf("first op should dismiss any open menu, got %v", ops)
	}
}

// Reconnecting the same server twice produces the same sentence twice, so the
// second run must notice a new copy of a line it has already seen.
func TestMCPReconnectRepeatSeesSecondIdenticalResult(t *testing.T) {
	once := `❯ /mcp
  ⎿  Failed to reconnect to agent-dashboard-channel (detail withheld on this connection).`
	twice := once + "\n" + once

	h := newHarness("mcp", "reconnect", "agent-dashboard-channel")
	h.fake.Screens = append([]string{once},
		settled([]string{screenList, screenDetailFailed, twice})...)
	h.env.Sleep = func(time.Duration) {}
	h.code = h.run()

	if h.code != 1 {
		t.Fatalf("exit %d, want 1: out=%q err=%q", h.code, h.out(), h.errs())
	}
	if !strings.Contains(h.errs(), "failed to reconnect") {
		t.Fatalf("stderr = %q", h.errs())
	}
}
