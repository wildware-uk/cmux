package menu

import "testing"

// A numbered menu offering both an auto-continue wait and a paid option: the
// auto-continue one wins, because it resumes the session without a human.
func TestPickWaitPrefersContinueAutomatically(t *testing.T) {
	lines := Lines(`   You've reached your usage limit

   ❯ 1. Upgrade your plan
     2. Wait here, then continue automatically at 6:00pm
     3. Stop and wait for limit to reset

   ↑/↓ to navigate · Enter to select`)

	got, ok := PickWait(lines)
	if !ok {
		t.Fatal("no choice made")
	}
	if got.Number != 2 {
		t.Fatalf("chose %d (%q), want 2", got.Number, got.Label)
	}
}

// With no auto-continue option, a plain wait is still better than nothing.
func TestPickWaitFallsBackToPlainWait(t *testing.T) {
	lines := Lines(`   ❯ 1. Upgrade your plan
     2. Wait for limit to reset`)

	got, ok := PickWait(lines)
	if !ok {
		t.Fatal("no choice made")
	}
	if got.Number != 2 || got.Wait != true {
		t.Fatalf("chose %d (%q)", got.Number, got.Label)
	}
}

// A menu that only offers ways to spend money must be left alone. This is the
// one that matters: pressing here charges someone.
func TestPickWaitRefusesSpendOnlyMenu(t *testing.T) {
	lines := Lines(`   You've reached your usage limit

   ❯ 1. Upgrade your plan
     2. Add funds`)

	if choice, ok := PickWait(lines); ok {
		t.Fatalf("chose %q, want nothing", choice.Label)
	}
	if !IsLimitMenu(lines) {
		t.Fatal("menu should still be recognised as the limit menu")
	}
}

// An unfamiliar screen is not a menu to press keys at.
func TestPickWaitRefusesUnknownScreen(t *testing.T) {
	lines := Lines(`   Some new dialog

   ❯ 1. Do the thing
     2. Do the other thing`)

	if _, ok := PickWait(lines); ok {
		t.Fatal("pressed at a screen it does not recognise")
	}
	if IsLimitMenu(lines) {
		t.Fatal("should not be taken for the limit menu")
	}
}

// Without numbers the cursor has to be stepped, and stepping is only safe if
// every option in between is one cmux can see. An unrecognised row between two
// known ones makes the count wrong, so it refuses.
func TestPickWaitRefusesUnnumberedMenuWithAnUnknownRowBetween(t *testing.T) {
	lines := Lines(`   ❯ Upgrade your plan
     Something cmux has never seen
     Wait for limit to reset`)

	if choice, ok := PickWait(lines); ok {
		t.Fatalf("chose %q with an unreadable row in the way", choice.Label)
	}
}

// An unnumbered menu cmux can read fully is fine to step through.
func TestUnnumberedMenuStepsToTheWaitOption(t *testing.T) {
	lines := Lines(`   ❯ Upgrade your plan
     Wait here, then continue automatically when the limit resets`)

	choice, ok := PickWait(lines)
	if !ok {
		t.Fatal("no choice made")
	}
	opts := LimitOptions(lines)
	keys := StepKeys(LimitCaret(opts), choice.Index)
	if len(keys) != 1 || keys[0] != "Down" {
		t.Fatalf("keys = %v, want one Down", keys)
	}
}
