package cli

import (
	"context"
	"strings"
	"testing"
)

const screenLimit = `   You've reached your usage limit

   ❯ 1. Upgrade your plan
     2. Wait here, then continue automatically at 6:00pm
     3. Stop and wait for limit to reset

   ↑/↓ to navigate · Enter to select`

const screenSpendOnly = `   You've reached your usage limit

   ❯ 1. Upgrade your plan
     2. Add funds`

const screenIdle = `❯ 
  ? for shortcuts`

func watchRun(t *testing.T, screens []string, args ...string) *harness {
	t.Helper()
	h := newHarness(append([]string{"watch", "--once", "--interval", "1ms"}, args...)...)
	h.fake.Screens = screens
	h.code = h.run()
	return h
}

func TestWatchChoosesTheAutoContinueOption(t *testing.T) {
	h := watchRun(t, []string{screenIdle, screenLimit})
	if h.code != 0 {
		t.Fatalf("exit %d: %s", h.code, h.errs())
	}
	ops := strings.Join(h.ops(), "\n")
	if !strings.Contains(ops, "send-keys -t %1 2") {
		t.Fatalf("ops = %s", ops)
	}
	if !strings.Contains(h.out(), "continue automatically") {
		t.Fatalf("output should log what it chose, got %q", h.out())
	}
}

// The one that must never go wrong: nothing safe on screen, nothing pressed.
func TestWatchPressesNothingOnASpendOnlyMenu(t *testing.T) {
	h := watchRun(t, []string{screenSpendOnly})
	if h.code != 0 {
		t.Fatalf("exit %d: %s", h.code, h.errs())
	}
	if ops := h.ops(); len(ops) != 0 {
		t.Fatalf("pressed %v, want nothing", ops)
	}
	if !strings.Contains(h.out(), "nothing safe to press") {
		t.Fatalf("output = %q", h.out())
	}
}

func TestWatchDryRunPressesNothing(t *testing.T) {
	h := watchRun(t, []string{screenLimit}, "--dry-run")
	if ops := h.ops(); len(ops) != 0 {
		t.Fatalf("pressed %v, want nothing", ops)
	}
	if !strings.Contains(h.out(), "would choose") {
		t.Fatalf("output = %q", h.out())
	}
}

// The menu stays on screen for a moment after the key lands. Pressing again
// would move the selection off the option cmux just chose.
func TestWatchAnswersOneMenuOnlyOnce(t *testing.T) {
	h := newHarness("watch", "--interval", "1ms")
	h.fake.Screens = []string{screenLimit, screenLimit, screenLimit, screenIdle}

	// Stop the loop after a few polls, the way Ctrl-C would.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.fake.OnCapture = func(n int) {
		if n >= 4 {
			cancel()
		}
	}
	h.env.Ctx = ctx
	h.code = h.run()

	if n := strings.Count(strings.Join(h.ops(), "\n"), "send-keys"); n != 1 {
		t.Fatalf("pressed %d times, want 1", n)
	}
}

func TestIntervalFlagIsRejectedElsewhere(t *testing.T) {
	h := newHarness("compact", "--interval", "5s")
	if code := h.run(); code != exitUsage {
		t.Fatalf("exit %d, want %d", code, exitUsage)
	}
	if !strings.Contains(h.errs(), "only for watch") {
		t.Fatalf("stderr = %q", h.errs())
	}
}
