package menu

import (
	"regexp"
	"strconv"
	"strings"
)

// The usage-limit menu, as Claude Code renders it.
//
// These are the whole contract between cmux and a screen it does not control,
// so they live together here with fixture tests: when Claude Code rewords the
// menu, this is the one list to change.
//
// waitLabels are ordered best first. "Wait here, then continue automatically"
// is the one worth having, because the session resumes on its own; the others
// stop until a human comes back.
var waitLabels = []string{
	"Wait here, then continue automatically",
	"Stop and wait for limit to reset",
	"Wait for limit to reset",
}

// spendLabels cost money. cmux recognises them only so it can refuse them, and
// so it can tell a menu it understands from one it does not.
var spendLabels = []string{
	"Upgrade your plan",
	"Add funds",
}

// LimitChoice is one option on the usage-limit menu.
type LimitChoice struct {
	Label string
	// Number is the digit to press, or 0 when the menu draws no numbers.
	Number int
	// Index is the option's position in the menu, which is what arrow keys count.
	Index int
	Line  int
	// Selected reports whether the cursor is on this option.
	Selected bool
	// Wait is true for options that only wait, false for ones that spend money.
	Wait bool
	// rank orders the wait options, best first.
	rank int
}

// optionPrefix strips the cursor and any leading number from a menu line.
var optionPrefix = regexp.MustCompile(`^(?:` + Caret + `\s*)?(?:(\d+)\.\s*)?`)

// LimitOptions reads the usage-limit menu's options off the pane.
//
// Only lines cmux recognises are returned. Anything else on screen — the
// explanation above the menu, the keyboard footer, the transcript — is ignored,
// which is also why an unfamiliar menu comes back empty rather than guessed at.
func LimitOptions(lines []string) []LimitChoice {
	var out []LimitChoice
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		selected := strings.HasPrefix(trimmed, Caret)

		m := optionPrefix.FindStringSubmatch(trimmed)
		number := 0
		if m[1] != "" {
			number, _ = strconv.Atoi(m[1])
		}
		text := strings.TrimSpace(trimmed[len(m[0]):])
		if text == "" {
			continue
		}

		choice, ok := classify(text)
		if !ok {
			continue
		}
		choice.Number = number
		choice.Index = len(out)
		choice.Line = i
		choice.Selected = selected
		out = append(out, choice)
	}
	return out
}

func classify(text string) (LimitChoice, bool) {
	lower := strings.ToLower(text)
	for rank, want := range waitLabels {
		if strings.Contains(lower, strings.ToLower(want)) {
			return LimitChoice{Label: text, Wait: true, rank: rank}, true
		}
	}
	for _, want := range spendLabels {
		if strings.Contains(lower, strings.ToLower(want)) {
			return LimitChoice{Label: text, Wait: false}, true
		}
	}
	return LimitChoice{}, false
}

// IsLimitMenu reports whether the pane is showing the usage-limit menu.
func IsLimitMenu(lines []string) bool {
	return len(LimitOptions(lines)) > 0
}

// PickWait chooses the option that waits, preferring one that then continues on
// its own.
//
// It returns false when there is nothing safe to press: a menu offering only
// ways to spend money, or one whose options are not laid out the way cmux
// expects. Doing nothing is the right answer to a screen cmux does not
// recognise — the cost of a wrong press here is a charge on someone's account.
func PickWait(lines []string) (LimitChoice, bool) {
	opts := LimitOptions(lines)
	if len(opts) == 0 {
		return LimitChoice{}, false
	}

	best := LimitChoice{}
	found := false
	for _, o := range opts {
		if !o.Wait {
			continue
		}
		if !found || o.rank < best.rank {
			best, found = o, true
		}
	}
	if !found {
		return LimitChoice{}, false
	}

	// An unnumbered menu is reached by stepping the cursor, and stepping is only
	// safe if cmux can see every option it would step past. A gap between two
	// recognised options means there is a row in between that cmux cannot name,
	// so the count would be wrong and the press would land somewhere else.
	if best.Number == 0 && !contiguous(lines, opts) {
		return LimitChoice{}, false
	}
	return best, true
}

func contiguous(lines []string, opts []LimitChoice) bool {
	for i := 1; i < len(opts); i++ {
		for l := opts[i-1].Line + 1; l < opts[i].Line; l++ {
			if strings.TrimSpace(lines[l]) != "" {
				return false
			}
		}
	}
	return true
}

// LimitCaret returns the position of the selected option, or -1.
func LimitCaret(opts []LimitChoice) int {
	for _, o := range opts {
		if o.Selected {
			return o.Index
		}
	}
	return -1
}
