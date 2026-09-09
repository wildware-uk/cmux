package cli

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/wildware-uk/cmux/internal/discover"
	"github.com/wildware-uk/cmux/internal/inject"
	"github.com/wildware-uk/cmux/internal/menu"
)

// How long to wait for a menu to appear, and how often to look.
const (
	menuTimeout  = 10 * time.Second
	menuInterval = 300 * time.Millisecond
	// After choosing Reconnect the menu closes and the outcome is printed into
	// the transcript. Connecting a server can be slow, so this waits longer.
	resultTimeout = 30 * time.Second
)

// Screen markers used to tell which view the pane is showing.
//
// Each view is identified by its heading *and* its keyboard footer. The footer
// matters: a heading can linger in the scrollback from an earlier run, and
// matching that would drive keys at a menu that is not open.
const (
	markerServerList   = "Manage MCP servers"
	markerServerDetail = "MCP Server"
	markerListFooter   = "Enter to confirm"
	markerDetailFooter = "Enter to select"
	actionReconnect    = "Reconnect"
)

// reconnectResult matches the outcome line for one specific server.
//
// The trailing group is what stops "agent-dashboard" matching the line about
// "agent-dashboard-channel" — the same prefix collision guarded against when
// picking the row, which is just as wrong when reading the result. A full stop
// is allowed to follow, because Claude Code ends the sentence with one.
func reconnectResult(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?:Failed to reconnect to|Reconnected to) ` +
		regexp.QuoteMeta(name) + `(?:[^\w-]|$)`)
}

// countLines counts the outcome lines for one server currently on screen.
//
// Counting rather than comparing the text is what makes a repeat work: ask
// twice and the second answer is the same sentence as the first, so "has the
// line changed?" says no forever. "Is there one more of them?" says yes.
func countLines(lines []string, re *regexp.Regexp) int {
	n := 0
	for _, l := range lines {
		if re.MatchString(l) {
			n++
		}
	}
	return n
}

func lastLine(lines []string, re *regexp.Regexp) string {
	last := ""
	for _, l := range lines {
		if re.MatchString(l) {
			last = l
		}
	}
	return last
}

// driveMCPReconnect walks the /mcp menus to reconnect one server.
//
// The flow was measured against a live pane, and two details drive the design.
// The server list is not number-selectable, so the right row is reached by
// stepping the cursor. The action menu is number-selectable, but the digit
// beside Reconnect moves depending on whether the server is currently
// connected, so it is read off the screen rather than assumed.
func driveMCPReconnect(ctx context.Context, r *run, pane discover.Pane) error {
	name := r.args[0]

	if r.opts.dryRun {
		fmt.Fprintf(r.out, "would open /mcp on %s and reconnect %q\n", pane.ID, name)
		return nil
	}

	// Clear any dialog a previous run left open, so /mcp starts from the prompt.
	r.escape(ctx, pane.ID)

	// Remember what the outcome line looked like before we touched anything, so
	// a result left over from an earlier run is not read as this one's.
	result := reconnectResult(name)
	before := 0
	if pre, err := r.client.CapturePane(ctx, pane.ID); err == nil {
		before = countLines(menu.Lines(pre), result)
	}

	// Open the menu.
	if err := inject.Run(ctx, r.client, pane.ID, inject.Text("/mcp")); err != nil {
		return err
	}

	lines, err := r.awaitStable(ctx, pane.ID, menuTimeout, func(l []string) bool {
		return menu.Contains(l, markerServerList) && menu.Contains(l, markerListFooter)
	})
	if err != nil {
		return fmt.Errorf("the /mcp server list did not appear: %w", err)
	}

	// Pick the server by stepping the cursor onto its row. Only the menu block
	// is searched: the transcript above it is full of "❯ /mcp" echoes that would
	// otherwise be mistaken for the cursor.
	view := menu.Menu(lines, markerServerList)
	row, err := menu.FindRow(view, name)
	if err != nil {
		r.escape(ctx, pane.ID) // leave the pane as we found it
		return err
	}
	if keys := menu.StepKeys(menu.CaretRow(view), row.Index); len(keys) > 0 {
		if err := r.client.SendKeys(ctx, pane.ID, keys...); err != nil {
			return err
		}
	}
	if err := r.client.SendKeys(ctx, pane.ID, "Enter"); err != nil {
		return err
	}

	// Pick the action by its number, read from the rendered menu.
	lines, err = r.awaitStable(ctx, pane.ID, menuTimeout, func(l []string) bool {
		return menu.Contains(l, markerServerDetail) && menu.Contains(l, markerDetailFooter) &&
			len(menu.Options(l)) > 0
	})
	if err != nil {
		return fmt.Errorf("the server detail view did not appear: %w", err)
	}
	opt, err := menu.FindOption(menu.Menu(lines, markerServerDetail), actionReconnect)
	if err != nil {
		r.escape(ctx, pane.ID)
		return err
	}
	if err := r.client.SendKeys(ctx, pane.ID, strconv.Itoa(opt.Number)); err != nil {
		return err
	}

	return r.reportReconnect(ctx, pane, name, result, before)
}

// reportReconnect reads the outcome out of the transcript, so cmux says what
// actually happened rather than that it pressed a key.
//
// before is how many outcome lines for this server were already on screen when
// the command started; the answer is the one that takes the count past it.
func (r *run) reportReconnect(ctx context.Context, pane discover.Pane, name string, result *regexp.Regexp, before int) error {
	var line string
	_, err := r.await(ctx, pane.ID, resultTimeout, func(l []string) bool {
		if countLines(l, result) <= before {
			return false
		}
		line = lastLine(l, result)
		return true
	})
	if err != nil {
		// Nothing conclusive on screen. Do not claim success.
		fmt.Fprintf(r.out, "asked %s to reconnect %s; no result on screen after %s\n",
			pane.ID, name, resultTimeout)
		return nil
	}
	if strings.Contains(line, "Failed to reconnect") {
		return fmt.Errorf("%s failed to reconnect (see the pane for details)", name)
	}
	fmt.Fprintf(r.out, "reconnected %s on %s (%s)\n", name, pane.ID, pane.Location())
	return nil
}

// await polls the pane until want is satisfied, or gives up.
// The budget is counted in polls rather than wall-clock time, so a test that
// stubs out sleep finishes instantly instead of spinning for the whole limit.
func (r *run) await(ctx context.Context, target string, limit time.Duration, want func([]string) bool) ([]string, error) {
	tries := int(limit / menuInterval)
	for i := 0; ; i++ {
		pane, err := r.client.CapturePane(ctx, target)
		if err != nil {
			return nil, err
		}
		lines := menu.Lines(pane)
		if want(lines) {
			return lines, nil
		}
		if i >= tries {
			return nil, fmt.Errorf("gave up after %s", limit)
		}
		r.sleep(menuInterval)
	}
}

// awaitStable waits for a menu that has finished drawing: the condition holds
// on two captures in a row that are identical.
//
// Acting on the first frame that looks right is a race. A menu that has only
// just appeared drops the keypress that follows it, leaving the pane sitting on
// the list while cmux waits for a detail view that will never come.
func (r *run) awaitStable(ctx context.Context, target string, limit time.Duration, want func([]string) bool) ([]string, error) {
	prev := ""
	return r.await(ctx, target, limit, func(l []string) bool {
		now := strings.Join(l, "\n")
		settled := prev == now
		prev = now
		return settled && want(l)
	})
}

// escape backs out of a menu, so a failed command does not strand the pane in
// a half-navigated dialog.
func (r *run) escape(ctx context.Context, target string) {
	_ = r.client.SendKeys(ctx, target, "Escape")
}
