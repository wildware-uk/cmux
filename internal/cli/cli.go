// Package cli is the cmux command line.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/wildware-uk/cmux/internal/discover"
	"github.com/wildware-uk/cmux/internal/inject"
	"github.com/wildware-uk/cmux/internal/tmux"
)

// BuildInfo is stamped in at release time.
type BuildInfo struct{ Version, Commit, Date string }

// Exit codes.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

// options are the flags shared by every command.
type options struct {
	to       string
	allPanes bool
	dryRun   bool
	defer_   int
	self     bool
	help     bool
	version  bool
}

// run carries everything a command needs.
type run struct {
	opts   options
	args   []string
	cmd    *Command
	finder Finder
	client tmux.Client
	build  BuildInfo
	out    io.Writer
	errOut io.Writer

	currentPane string
}

// Finder is the subset of discover.Finder the commands use, so tests can
// substitute one.
type Finder interface {
	All(ctx context.Context) ([]discover.Pane, error)
	Find(ctx context.Context) ([]discover.Pane, error)
	Resolve(ctx context.Context, spec string) (discover.Pane, error)
}

// Env is the outside world, injected so tests can replace it.
type Env struct {
	Args        []string
	Stdout      io.Writer
	Stderr      io.Writer
	Client      tmux.Client
	Finder      Finder
	Build       BuildInfo
	CurrentPane string
	Self        string // path used when building a --defer command line
}

// Main runs cmux against the real world.
func Main(b BuildInfo) int {
	client := tmux.Exec{}
	self, err := os.Executable()
	if err != nil {
		self = "cmux"
	}
	return Run(Env{
		Args:        os.Args[1:],
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		Client:      client,
		Finder:      discover.New(client),
		Build:       b,
		CurrentPane: os.Getenv("TMUX_PANE"),
		Self:        self,
	})
}

// Run is Main with the world passed in.
func Run(env Env) int {
	opts, rest, err := parseGlobal(env.Args)
	if err != nil {
		fmt.Fprintln(env.Stderr, "cmux:", err)
		fmt.Fprintln(env.Stderr, "run 'cmux --help' for usage")
		return exitUsage
	}

	if opts.version {
		fmt.Fprintf(env.Stdout, "cmux %s (%s, built %s)\n", env.Build.Version, env.Build.Commit, env.Build.Date)
		return exitOK
	}

	if len(rest) == 0 {
		if opts.help {
			writeRootHelp(env.Stdout)
			return exitOK
		}
		writeRootHelp(env.Stderr)
		return exitUsage
	}

	name, args := rest[0], rest[1:]
	cmd := lookup(name)
	if cmd == nil {
		fmt.Fprintf(env.Stderr, "cmux: unknown command %q\n", name)
		fmt.Fprintln(env.Stderr, "run 'cmux --help' for the list of commands")
		return exitUsage
	}

	// For commands that do not take free text, flags may also follow the
	// subcommand. For goal and friends everything after the name is literal.
	if !cmd.FreeText {
		more, tail, err := parseGlobalInto(opts, args)
		if err != nil {
			fmt.Fprintln(env.Stderr, "cmux:", err)
			return exitUsage
		}
		opts, args = more, tail
	} else {
		args = stripLeadingDashDash(args)
	}

	if opts.help {
		writeCommandHelp(env.Stdout, cmd)
		return exitOK
	}

	if cmd.ArgRequired && len(args) == 0 {
		fmt.Fprintf(env.Stderr, "cmux: %s needs an argument\n", cmd.Name)
		fmt.Fprintf(env.Stderr, "usage: %s\n", cmd.Usage())
		return exitUsage
	}
	if cmd.Arg == "" && len(args) > 0 {
		fmt.Fprintf(env.Stderr, "cmux: %s takes no arguments, got %q\n", cmd.Name, strings.Join(args, " "))
		return exitUsage
	}

	r := &run{
		opts: opts, args: args, cmd: cmd,
		finder: env.Finder, client: env.Client, build: env.Build,
		out: env.Stdout, errOut: env.Stderr,
		currentPane: env.CurrentPane,
	}
	if r.finder == nil {
		r.finder = discover.New(env.Client)
	}

	if cmd.Local != nil {
		if err := cmd.Local(r); err != nil {
			return report(env.Stderr, err)
		}
		return exitOK
	}

	if err := send(context.Background(), r, env); err != nil {
		return report(env.Stderr, err)
	}
	return exitOK
}

func report(w io.Writer, err error) int {
	fmt.Fprintln(w, "cmux:", err)
	return exitError
}

// send resolves the target and delivers the command's keystrokes.
func send(ctx context.Context, r *run, env Env) error {
	pane, err := r.finder.Resolve(ctx, r.opts.to)
	if err != nil {
		return err
	}
	if !pane.IsClaude && !r.opts.allPanes {
		return discover.NotClaudeError{Pane: pane}
	}

	targetingSelf := pane.ID == env.CurrentPane && env.CurrentPane != ""
	if targetingSelf && r.cmd.SelfNeedsFlag && !r.opts.self {
		return fmt.Errorf("%s would cancel the turn that is running cmux; pass --self if you mean it", r.cmd.Name)
	}

	var ops []inject.Op
	var what string
	if r.cmd.Key != "" {
		ops = inject.Key(r.cmd.Key)
		what = r.cmd.Key
	} else {
		text := r.cmd.Slash
		if len(r.args) > 0 {
			text += " " + strings.Join(r.args, " ")
		}
		ops = inject.Text(text)
		what = inject.Sanitise(text)
	}

	if targetingSelf && r.cmd.Destructive && r.opts.defer_ == 0 {
		fmt.Fprintf(r.errOut,
			"cmux: warning: %s fires immediately, and this pane is mid-turn while cmux runs; consider --defer\n",
			r.cmd.Name)
	}

	if r.opts.dryRun {
		for _, op := range ops {
			fmt.Fprintln(r.out, strings.ReplaceAll(op.String(), "TARGET", pane.ID))
		}
		return nil
	}

	if r.opts.defer_ > 0 {
		line := deferCommandLine(env.Self, pane.ID, r)
		if err := r.client.RunShellDetached(ctx, r.opts.defer_, line); err != nil {
			return err
		}
		fmt.Fprintf(r.out, "scheduled %s for %s in %ds\n", what, pane.ID, r.opts.defer_)
		return nil
	}

	if err := inject.Run(ctx, r.client, pane.ID, ops); err != nil {
		return err
	}
	fmt.Fprintf(r.out, "sent %s to %s (%s)\n", what, pane.ID, pane.Location())
	return nil
}

// deferCommandLine rebuilds this invocation as a command tmux can run later,
// with the target pinned and --defer dropped so it does not reschedule itself.
func deferCommandLine(self, target string, r *run) string {
	parts := []string{shellQuote(self), "--to", target}
	if r.opts.allPanes {
		parts = append(parts, "--all-panes")
	}
	if r.opts.self {
		parts = append(parts, "--self")
	}
	parts = append(parts, r.cmd.Name)
	for _, a := range r.args {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func stripLeadingDashDash(args []string) []string {
	if len(args) > 0 && args[0] == "--" {
		return args[1:]
	}
	return args
}

func parseGlobal(args []string) (options, []string, error) {
	return parseGlobalInto(options{}, args)
}

// parseGlobalInto parses global flags, starting from what was already set, and
// stops at the first non-flag argument.
func parseGlobalInto(start options, args []string) (options, []string, error) {
	opts := start
	fs := flag.NewFlagSet("cmux", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}

	to := fs.String("to", opts.to, "")
	allPanes := fs.Bool("all-panes", opts.allPanes, "")
	dryRun := fs.Bool("dry-run", opts.dryRun, "")
	deferFor := fs.String("defer", strconv.Itoa(opts.defer_), "")
	self := fs.Bool("self", opts.self, "")
	help := fs.Bool("help", opts.help, "")
	helpShort := fs.Bool("h", opts.help, "")
	version := fs.Bool("version", opts.version, "")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			opts.help = true
			return opts, fs.Args(), nil
		}
		return opts, nil, err
	}

	seconds, err := strconv.Atoi(*deferFor)
	if err != nil || seconds < 0 {
		return opts, nil, fmt.Errorf("--defer wants a whole number of seconds, got %q", *deferFor)
	}

	opts.to = *to
	opts.allPanes = *allPanes
	opts.dryRun = *dryRun
	opts.defer_ = seconds
	opts.self = *self
	opts.help = *help || *helpShort
	opts.version = *version
	return opts, fs.Args(), nil
}
