package cli

import (
	"context"
	"strings"

	"github.com/wildware-uk/cmux/internal/discover"
)

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
	// Drive is set for commands that walk an interactive menu rather than
	// sending a single payload. The target is already resolved and checked.
	Drive func(context.Context, *run, discover.Pane) error
	// Sub holds nested subcommands, as in "cmux mcp reconnect".
	Sub []*Command
	// Flags are extra flags this command alone accepts, shown in its help above
	// the shared ones.
	Flags [][2]string
	// Path is the full invocation for a nested command, e.g. "mcp reconnect".
	// Empty for top-level commands, which are named by Name alone.
	Path string

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
		{
			Name: "rate-limit-options", Slash: "/rate-limit-options",
			Summary: "Arm the target to wait out a usage limit and carry on",
			Long: "Sends /rate-limit-options, which opens Claude Code's own menu for what to do\n" +
				"when you hit a usage limit.\n\n" +
				"Arming the auto-resume option there is the reliable way to stop a session\n" +
				"stalling overnight: it waits for the limit to reset and continues by itself,\n" +
				"with no watching and no screen scraping. Prefer it over cmux watch, which\n" +
				"exists only for sessions that reached the limit without being armed.",
		},
		{
			Name: "watch", Drive: driveWatch,
			Flags: [][2]string{
				{"--interval <duration>", "how often to look at the pane (default 15s)"},
				{"--once", "stop after the first time the menu is dealt with"},
			},
			Summary: "Wait out usage limits on a pane, unattended",
			Long: "Watches a pane and, when Claude Code's usage-limit menu appears, chooses the\n" +
				"option that waits — preferring one that then continues automatically, so the\n" +
				"session resumes without a human.\n\n" +
				"It never presses Upgrade your plan or Add funds. If the menu is not one cmux\n" +
				"recognises, it says so and presses nothing: a wrong press here costs money.\n\n" +
				"Prefer cmux rate-limit-options. Arming that ahead of time means the menu never\n" +
				"blocks in the first place; watch is the net for when it was not armed.\n\n" +
				"This is the only cmux command that keeps running. It reads the menu off the\n" +
				"screen, so a change to how Claude Code draws it will break watch — the\n" +
				"wording it looks for is one list in internal/menu/limit.go.",
		},
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
			Name: "mcp", Arg: "<subcommand>",
			Summary: "Manage the target's MCP servers",
			Long:    "Drives the target's /mcp menu.",
			Sub: []*Command{
				{
					Name: "reconnect", Path: "mcp reconnect", Arg: "<server>", ArgRequired: true,
					Drive:   driveMCPReconnect,
					Summary: "Reconnect one of the target's MCP servers",
					Long: "Opens /mcp, selects the named server, and chooses Reconnect.\n\n" +
						"The server name must match exactly. Names nest — agent-dashboard is a\n" +
						"prefix of agent-dashboard-channel — so a partial name is refused rather\n" +
						"than guessed at.\n\n" +
						"Exits non-zero if the reconnect failed, reading the outcome from the\n" +
						"pane rather than assuming the keypress worked.",
				},
			},
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

// findSub looks up a nested subcommand by name.
func (c *Command) findSub(name string) *Command {
	for _, s := range c.Sub {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// subNames lists the nested subcommands, for error messages and help.
func (c *Command) subNames() string {
	var names []string
	for _, s := range c.Sub {
		names = append(names, s.Name)
	}
	return strings.Join(names, ", ")
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
	name := c.Name
	if c.Path != "" {
		name = c.Path
	}
	if c.Arg == "" {
		return "cmux " + name
	}
	return "cmux " + name + " " + c.Arg
}
