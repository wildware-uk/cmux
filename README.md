# cmux

Let Claude drive its own Claude Code session.

Claude cannot type into its own terminal. But it can run a shell command, and a
shell command can tell tmux to type. That is all cmux is: a small Go binary that
turns `cmux compact` into the tmux calls that put `/compact` into a pane.

```sh
cmux goal Ship the parser rewrite
cmux compact
cmux clear
cmux panes
cmux --to %12 interrupt
```

## Install

```sh
go install github.com/wildware-uk/cmux@latest
```

Or download a binary for Linux or macOS from the
[releases page](https://github.com/wildware-uk/cmux/releases).

You need tmux, and you need to be inside it.

## How it works

Every invocation does the same four things: work out which pane to talk to, build
a keystroke payload, hand it to tmux, and exit. There is no daemon, no config
file and no state on disk.

```
tmux load-buffer -   <- the text
tmux paste-buffer    <- into the pane
tmux send-keys Enter <- submit
```

Panes are found by looking for a `claude` process: first by checking the pane's
own command, then by walking the process tree, which catches Claude started under
a shell wrapper. `cmux panes` shows what it found.

## Commands

| Command | What it sends |
|---|---|
| `cmux goal <text>` | `/goal <text>` |
| `cmux compact` | `/compact` |
| `cmux clear` | `/clear` |
| `cmux model <name>` | `/model <name>` |
| `cmux resume` | `/resume` |
| `cmux context` | `/context` |
| `cmux cost` | `/cost` |
| `cmux agents` | `/agents` |
| `cmux interrupt` | a bare Escape key |
| `cmux panes` | nothing — lists the panes cmux can see |
| `cmux status` | nothing — shows where you are and what a bare command would hit |

The set is closed on purpose. An unknown subcommand is an error, not something
passed through.

## Flags

| Flag | Meaning |
|---|---|
| `--to <target>` | pane id (`%12`), `session:window.pane`, or an index from `cmux panes` |
| `--all-panes` | allow a target that was not detected as Claude Code |
| `--dry-run` | print the tmux calls instead of making them |
| `--defer <seconds>` | have tmux deliver the keys later, through `run-shell` |
| `--self` | allow `interrupt` to target the pane cmux was run from |

For `goal`, everything after the subcommand is taken literally, so quotes and
dashes need no escaping.

## The one thing to know

**Slash commands fire immediately. They do not wait for the target to finish.**

This was measured, not assumed — see [docs/injection.md](docs/injection.md).
`/clear` sent to a pane running a 30-second shell command wiped that session's
history straight away, while the shell was still running.

It matters because of how cmux is meant to be used. When Claude runs `cmux
compact` from a tool call, its own turn is still in flight by definition. So a
self-targeted `compact` or `clear` fires in exactly that situation. cmux prints a
warning when it notices, and `--defer 10` schedules the keys for later if you
want to get out of the way first.

`interrupt` is the sharpest version of this: Escape takes effect the instant it
lands, so pointing it at your own pane needs `--self`.

Plain text behaves differently — it waits in the input box until the current turn
ends. Only slash commands jump the queue.

## About `/goal`

`/goal` is not a built-in Claude Code command. `cmux goal` assumes you have it as
a custom slash command. Without one, the target will report it as unknown.

## Caveats

cmux drives a terminal UI by typing into it. That is inherently a bit brittle: a
change to how Claude Code renders its input box or its slash-command menu could
break it. The findings in `docs/injection.md` were measured against Claude Code
v2.1.266 and tmux 3.4.

## Development

```sh
go test ./...                      # unit tests, no tmux needed
go test -tags integration ./...    # drives a real tmux server on its own socket
go test ./internal/cli -update     # rewrite the golden help files
```

## Licence

MIT
