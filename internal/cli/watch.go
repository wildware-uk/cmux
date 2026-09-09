package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/wildware-uk/cmux/internal/discover"
	"github.com/wildware-uk/cmux/internal/menu"
)

// driveWatch polls a pane and answers the usage-limit menu when it appears.
//
// This is the only cmux command that keeps running. Everything else types once
// and exits; watch sits there so a session that hits its limit unattended waits
// for the reset instead of stopping until somebody notices. Arming
// /rate-limit-options ahead of time is better, because then the menu never
// blocks in the first place — watch is the net for when it was not armed.
func driveWatch(ctx context.Context, r *run, pane discover.Pane) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(r.out, "%s watching %s (%s) every %s\n",
		stamp(), pane.ID, pane.Location(), r.opts.interval)

	// answered stops a menu being acted on twice: the pane keeps showing it for
	// a moment after the key lands, and pressing again would move the selection
	// off the option cmux just chose.
	answered := false

	for {
		screen, err := r.client.CapturePane(ctx, pane.ID)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			return err
		}
		lines := menu.Lines(screen)

		switch {
		case !menu.IsLimitMenu(lines):
			answered = false
		case answered:
			// Still on screen from the press cmux already made.
		default:
			acted, err := r.answerLimit(ctx, pane, lines)
			if err != nil {
				return err
			}
			answered = acted
			if r.opts.once {
				return nil
			}
		}

		if !wait(ctx, r.opts.interval) {
			break
		}
	}

	fmt.Fprintf(r.out, "%s stopped watching %s\n", stamp(), pane.ID)
	return nil
}

// answerLimit presses the option that waits, and reports whether it pressed
// anything. Refusing is a normal outcome, not an error: watch keeps watching.
func (r *run) answerLimit(ctx context.Context, pane discover.Pane, lines []string) (bool, error) {
	opts := menu.LimitOptions(lines)
	choice, ok := menu.PickWait(lines)
	if !ok {
		var seen []string
		for _, o := range opts {
			seen = append(seen, o.Label)
		}
		fmt.Fprintf(r.out, "%s limit menu on %s offers nothing safe to press, leaving it alone; saw: %v\n",
			stamp(), pane.ID, seen)
		return false, nil
	}

	if r.opts.dryRun {
		fmt.Fprintf(r.out, "%s would choose %q on %s\n", stamp(), choice.Label, pane.ID)
		return true, nil
	}

	// Numbered menus take the digit. Unnumbered ones are reached by stepping the
	// cursor, which PickWait has already checked is safe to count.
	var keys []string
	if choice.Number > 0 {
		keys = []string{strconv.Itoa(choice.Number)}
	} else {
		keys = append(menu.StepKeys(menu.LimitCaret(opts), choice.Index), "Enter")
	}
	if err := r.client.SendKeys(ctx, pane.ID, keys...); err != nil {
		return false, err
	}

	fmt.Fprintf(r.out, "%s chose %q on %s\n", stamp(), choice.Label, pane.ID)
	return true, nil
}

// wait sleeps for the poll interval, and reports false if an interrupt arrived
// first. A plain sleep would keep the process alive for up to a whole interval
// after Ctrl-C.
func wait(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func stamp() string { return time.Now().Format("15:04:05") }
