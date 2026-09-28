package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wildware-uk/cmux/internal/discover"
	"github.com/wildware-uk/cmux/internal/inject"
)

// How restart decides a pane is still. A running turn redraws at least once a
// second — the spinner turns and its timer counts — so five seconds with no
// change is a finished turn. An idle Claude Code pane does not redraw at all.
const (
	stillPoll    = 500 * time.Millisecond
	stillFor     = 10 // polls, so five seconds
	idleTimeout  = 3600 // polls, so thirty minutes
	clearTimeout = 120 // polls, so a minute for /clear to land and settle
)

// driveRestart waits for the target's turn to end, clears it, and sends the
// first prompt of the fresh session.
//
// Order is the whole point. /clear fires the moment it lands, even mid-turn
// (docs/injection.md), so it must wait for the turn to end. The prompt must
// wait for the clear, or it is either cleared away or submitted into the old
// session. Plain text sent any earlier would queue behind the running turn,
// go out when it ends, and start a new turn that /clear then lands in.
func driveRestart(ctx context.Context, r *run, pane discover.Pane) error {
	prompt := inject.Sanitise(strings.Join(r.args, " "))

	if r.opts.dryRun {
		fmt.Fprintf(r.out, "wait until %s has been still for %s\n", pane.ID, stillPoll*stillFor)
		for _, op := range append(inject.Text("/clear"), inject.Text(prompt)...) {
			fmt.Fprintln(r.out, strings.ReplaceAll(op.String(), "TARGET", pane.ID))
		}
		return nil
	}

	// Waiting inline on our own pane would never finish: the turn that ran
	// cmux cannot end while cmux holds its tool call open. So hand the whole
	// thing to tmux, which outlives the turn, and return.
	self := pane.ID == r.currentPane && r.currentPane != ""
	if !r.detached && (self || r.opts.defer_ > 0) {
		delay := max(r.opts.defer_, 1)
		if err := r.client.RunShellDetached(ctx, delay, deferCommandLine(pane.ID, r)); err != nil {
			return err
		}
		fmt.Fprintf(r.out, "scheduled restart of %s: once its turn ends it gets /clear, then %q\n", pane.ID, prompt)
		if r.logPath != "" {
			fmt.Fprintf(r.out, "progress goes to %s\n", r.logPath)
		}
		return nil
	}

	fmt.Fprintf(r.out, "%s restart %s: waiting for the turn to end\n", stamp(), pane.ID)
	screen, err := r.waitStill(ctx, pane, "", idleTimeout)
	if err != nil {
		return fmt.Errorf("%s never went still, so nothing was sent: %w", pane.ID, err)
	}

	if err := inject.Run(ctx, r.client, pane.ID, inject.Text("/clear")); err != nil {
		return err
	}
	fmt.Fprintf(r.out, "%s restart %s: sent /clear\n", stamp(), pane.ID)

	// Wait for the screen to change away from the old session and then settle.
	// A prompt pasted while the session is being rebuilt is lost.
	if _, err := r.waitStill(ctx, pane, screen, clearTimeout); err != nil {
		return fmt.Errorf("/clear did not visibly land on %s, so the prompt was not sent: %w", pane.ID, err)
	}

	if err := inject.Run(ctx, r.client, pane.ID, inject.Text(prompt)); err != nil {
		return err
	}
	fmt.Fprintf(r.out, "%s restart %s: sent %q\n", stamp(), pane.ID, prompt)
	return nil
}

// waitStill polls the pane until it shows the same screen stillFor times in a
// row, and returns that screen. When before is set, a still screen equal to it
// does not count: the caller is waiting for something to change first.
func (r *run) waitStill(ctx context.Context, pane discover.Pane, before string, limit int) (string, error) {
	last, same := "", 0
	for polls := 0; polls < limit; polls++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		screen, err := r.client.CapturePane(ctx, pane.ID)
		if err != nil {
			return "", err
		}
		if screen == last {
			same++
		} else {
			last, same = screen, 1
		}
		if same >= stillFor && (before == "" || screen != before) {
			return screen, nil
		}
		r.sleep(stillPoll)
	}
	return "", fmt.Errorf("gave up after %s", stillPoll*time.Duration(limit))
}
