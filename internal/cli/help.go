package cli

import (
	"fmt"
	"io"
	"text/tabwriter"
)

const rootIntro = `cmux relays Claude Code commands into tmux panes.

Claude cannot type into its own terminal, but it can run a shell command, and a
shell command can tell tmux to type. That is all cmux is.`

const rootNotes = `Notes:
  Slash commands take effect immediately, even if the target is mid-turn. That
  includes clear and compact, so aiming one at your own pane discards history
  while your turn is still running. Use --defer to get out of the way first.

  interrupt is different again: it sends a bare Escape, which is never queued
  and cancels the target's current turn on arrival.`

func writeRootHelp(w io.Writer) {
	fmt.Fprintln(w, rootIntro)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  cmux [flags] <command> [arguments]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, c := range commands() {
		fmt.Fprintf(tw, "  %s\t%s\n", c.Usage()[len("cmux "):], c.Summary)
	}
	tw.Flush()
	fmt.Fprintln(w)
	writeFlags(w)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Examples:")
	fmt.Fprintln(w, "  cmux goal Ship the parser rewrite")
	fmt.Fprintln(w, "  cmux compact")
	fmt.Fprintln(w, "  cmux panes")
	fmt.Fprintln(w, "  cmux --to %12 interrupt")
	fmt.Fprintln(w, "  cmux --defer 5 clear")
	fmt.Fprintln(w)
	fmt.Fprintln(w, rootNotes)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run 'cmux <command> --help' for more about one command.")
}

func writeFlags(w io.Writer) {
	fmt.Fprintln(w, "Flags:")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, f := range [][2]string{
		{"--to <target>", "pane id (%12), session:window.pane, or an index from cmux panes"},
		{"--all-panes", "allow a target that was not detected as Claude Code"},
		{"--dry-run", "print the tmux calls instead of making them"},
		{"--defer <seconds>", "have tmux deliver the keys later, via run-shell"},
		{"--self", "allow interrupt to target the pane cmux was run from"},
		{"-h, --help", "show help"},
		{"--version", "show version"},
	} {
		fmt.Fprintf(tw, "  %s\t%s\n", f[0], f[1])
	}
	tw.Flush()
}

func writeCommandHelp(w io.Writer, c *Command) {
	fmt.Fprintf(w, "%s\n\n", c.Summary)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintf(w, "  %s\n", c.Usage())
	if c.Long != "" {
		fmt.Fprintf(w, "\n%s\n", c.Long)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Example:")
	fmt.Fprintf(w, "  %s\n", example(c))
	fmt.Fprintln(w)
	writeFlags(w)
}

func example(c *Command) string {
	switch c.Name {
	case "goal":
		return "cmux goal Ship the parser rewrite"
	case "model":
		return "cmux model opus"
	case "interrupt":
		return "cmux --to %12 interrupt"
	case "panes":
		return "cmux panes --all-panes"
	case "status":
		return "cmux status"
	case "rate-limit-options":
		return "cmux --to %12 rate-limit-options"
	default:
		return "cmux " + c.Name
	}
}
