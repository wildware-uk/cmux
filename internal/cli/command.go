package cli

// Command is one cmux subcommand.
type Command struct {
	Name string
	// Slash is the Claude Code command this sends, e.g. "/compact".
	// Empty for commands that cmux answers itself.
	Slash string
	// Key is a raw key name to send instead of text, e.g. "Escape".
	Key string
	// Arg is the argument hint shown in help, e.g. "<text>". Empty means the
	// command takes no arguments.
	Arg string
	// ArgRequired fails the command when Arg is missing.
	ArgRequired bool
	// FreeText means everything after the subcommand is literal text, so it is
	// never parsed as flags.
	FreeText bool
	// Destructive commands change conversation state and fire immediately even
	// when the target is mid-turn, so aiming one at the calling pane is worth a
	// warning. See docs/injection.md.
	Destructive bool
	// SelfNeedsFlag refuses to target the calling pane without --self.
	SelfNeedsFlag bool
	// Local is set for commands cmux answers without touching the target.
	Local func(*run) error

	Summary string
	Long    string
}

// commands is the whole command surface. It is deliberately closed: an unknown
// subcommand is an error, not something passed through to Claude Code.
func commands() []*Command {
	return []*Command{
		{
			Name: "goal", Slash: "/goal", Arg: "<text>", ArgRequired: true, FreeText: true,
			Summary: "Set the target session's goal",
			Long: "Sends /goal followed by your text.\n\n" +
				"Everything after the subcommand is taken literally, so quotes, dashes and\n" +
				"punctuation need no escaping. Runs of whitespace are collapsed to single\n" +
				"spaces, because a newline in the payload would submit the message early.\n\n" +
				"Note that /goal is not a built-in Claude Code command. It works if you have\n" +
				"it as a custom slash command; otherwise the target will report it as unknown.",
		},
		{
			Name: "compact", Slash: "/compact", Destructive: true,
			Summary: "Compact the target's conversation",
			Long: "Sends /compact.\n\n" +
				"This fires immediately, even if the target is in the middle of a turn. When\n" +
				"aimed at the pane you are calling from, that means it compacts while your own\n" +
				"turn is still running. cmux warns when it spots that; --defer buys you time.",
		},
		{
			Name: "clear", Slash: "/clear", Destructive: true,
			Summary: "Clear the target's conversation",
			Long: "Sends /clear.\n\n" +
				"Like compact, this fires immediately rather than waiting for the target to\n" +
				"finish. Clearing a session that is mid-tool-call discards its history while\n" +
				"the tool is still running.",
		},
		{
			Name: "model", Slash: "/model", Arg: "<name>", ArgRequired: true,
			Summary: "Switch the target's model",
			Long:    "Sends /model followed by the model name.",
		},
		{Name: "resume", Slash: "/resume", Summary: "Open the target's resume picker"},
		{Name: "context", Slash: "/context", Summary: "Show the target's context usage"},
		{Name: "cost", Slash: "/cost", Summary: "Show the target's cost and usage"},
		{Name: "agents", Slash: "/agents", Summary: "Open the target's agent list"},
		{
			Name: "interrupt", Key: "Escape", SelfNeedsFlag: true,
			Summary: "Cancel whatever the target is doing",
			Long: "Sends a bare Escape key.\n\n" +
				"Unlike every other command this is not text and is never queued: it takes\n" +
				"effect the instant it lands. Sent to your own pane it would cancel the very\n" +
				"turn that is running cmux, so that needs --self.",
		},
		{
			Name: "panes", Local: cmdPanes,
			Summary: "List the Claude Code panes cmux can see",
			Long: "Lists discovered Claude Code panes with an index you can pass to --to.\n\n" +
				"With --all-panes, lists every tmux pane and shows which ones were detected\n" +
				"as Claude Code.",
		},
		{
			Name: "status", Local: cmdStatus,
			Summary: "Show this pane, detection result and default target",
			Long:    "Answers \"where am I, and what would a bare cmux compact hit?\"",
		},
	}
}

func lookup(name string) *Command {
	for _, c := range commands() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// Usage is the one-line form shown in help.
func (c *Command) Usage() string {
	if c.Arg == "" {
		return "cmux " + c.Name
	}
	return "cmux " + c.Name + " " + c.Arg
}
