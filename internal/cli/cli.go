// Package cli is the cmux command line.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
	// interval and once belong to watch, the only long-running command.
	interval time.Duration
	once     bool
}

// defaultInterval is how often watch looks at the pane. Slow enough to be
// invisible, fast enough that a reset is not missed by much.
const defaultInterval = 15 * time.Second

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
	// self and logPath are what a command needs to hand itself to tmux; see
	// deferCommandLine. detached is true in the copy tmux runs.
	self     string
	logPath  string
	detached bool
	// sleep is the polling delay, replaced in tests so they do not wait.
	sleep func(time.Duration)
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
	// Ctx bounds long-running commands. nil means context.Background().
	Ctx         context.Context
	Args        []string
	Stdout      io.Writer
	Stderr      io.Writer
	Client      tmux.Client
	Finder      Finder
	Build       BuildInfo
	CurrentPane string
	Self        string // path used when building a --defer command line
	// LogPath is where a deferred command's output goes. Empty means the
	// deferred output is thrown away.
	LogPath string
	// Detached is set in a copy of cmux that tmux is running for --defer.
	Detached bool
	// Sleep replaces the polling delay in tests. nil means time.Sleep.
	Sleep func(time.Duration)
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
		LogPath:     filepath.Join(os.TempDir(), fmt.Sprintf("cmux-%d.log", os.Getuid())),
		Detached:    os.Getenv(detachedEnv) == "1",
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

	if len(cmd.Sub) > 0 {
		if len(args) == 0 || isFlag(args[0]) {
			if opts.help || (len(args) > 0 && args[0] == "--help") {
				writeCommandHelp(env.Stdout, cmd)
				return exitOK
			}
			fmt.Fprintf(env.Stderr, "cmux: %s needs a subcommand: %s\n", cmd.Name, cmd.subNames())
			return exitUsage
		}
		sub := cmd.findSub(args[0])
		if sub == nil {
			fmt.Fprintf(env.Stderr, "cmux: unknown %s subcommand %q; try: %s\n", cmd.Name, args[0], cmd.subNames())
			return exitUsage
		}
		cmd, args = sub, args[1:]
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

	// watch owns --interval and --once. Accepting them anywhere would let a
	// typo look like it was understood.
	if cmd.Name != "watch" {
		if opts.once {
			fmt.Fprintf(env.Stderr, "cmux: --once is only for watch\n")
			return exitUsage
		}
		if opts.interval != defaultInterval {
			fmt.Fprintf(env.Stderr, "cmux: --interval is only for watch\n")
			return exitUsage
		}
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
		self:        env.Self,
		logPath:     env.LogPath,
		detached:    env.Detached,
		sleep:       env.Sleep,
	}
	if r.sleep == nil {
		r.sleep = time.Sleep
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

	ctx := env.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := send(ctx, r, env); err != nil {
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

	if r.cmd.Drive != nil {
		return r.cmd.Drive(ctx, r, pane)
	}

	var ops []inject.Op
	var what string
	if r.cmd.Key != "" {
		ops = inject.Key(r.cmd.Key)
		what = r.cmd.Key
	} else {
		// prompt has no slash command, so the text is the arguments alone.
		text := strings.TrimSpace(r.cmd.Slash + " " + strings.Join(r.args, " "))
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
		line := deferCommandLine(pane.ID, r)
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

// detachedEnv marks the copy of cmux that tmux runs for --defer, so a command
// that detaches itself does not detach again.
const detachedEnv = "CMUX_DETACHED"

// deferCommandLine rebuilds this invocation as a command tmux can run later,
// with the target pinned and --defer dropped so it does not reschedule itself.
//
// Its output is redirected on purpose. tmux shows whatever a run-shell job
// prints in view mode over the current pane, and a pane in view mode takes
// every key sent after it — so a deferred "sent /clear" message would swallow
// the next command meant for Claude.
func deferCommandLine(target string, r *run) string {
	parts := []string{detachedEnv + "=1", shellQuote(r.self), "--to", target}
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
	if r.logPath == "" {
		parts = append(parts, ">/dev/null", "2>&1")
	} else {
		parts = append(parts, ">>"+shellQuote(r.logPath), "2>&1")
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func isFlag(s string) bool { return strings.HasPrefix(s, "-") }

func stripLeadingDashDash(args []string) []string {
	if len(args) > 0 && args[0] == "--" {
		return args[1:]
	}
	return args
}

func parseGlobal(args []string) (options, []string, error) {
	return parseGlobalInto(options{interval: defaultInterval}, args)
}

func durationFlag(d time.Duration) string {
	if d == 0 {
		return defaultInterval.String()
	}
	return d.String()
}

// parseInterval takes a duration, or a bare number read as seconds.
func parseInterval(s string) (time.Duration, error) {
	if n, err := strconv.Atoi(s); err == nil {
		s = strconv.Itoa(n) + "s"
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("--interval wants a duration like 15s, got %q", s)
	}
	return d, nil
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
	interval := fs.String("interval", durationFlag(opts.interval), "")
	once := fs.Bool("once", opts.once, "")

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
	opts.once = *once

	every, err := parseInterval(*interval)
	if err != nil {
		return opts, nil, err
	}
	opts.interval = every
	return opts, fs.Args(), nil
}
