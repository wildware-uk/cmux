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
| `cmux rate-limit-options` | `/rate-limit-options` |
| `cmux watch` | nothing until the usage-limit menu appears, then picks the wait option |
| `cmux mcp reconnect <server>` | `/mcp`, then picks the server and its Reconnect action |
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

## Waiting out a usage limit

`cmux watch` sits on a pane and, when Claude Code's usage-limit menu appears,
picks the option that waits — preferring one that then continues on its own, so
an unattended session resumes instead of stopping until someone notices.

```
cmux watch
cmux --to %12 watch --interval 10s --once
```

Two rules it will not break: it never presses **Upgrade your plan** or **Add
funds**, and it presses nothing at all on a menu it does not recognise. A wrong
press here costs money, so doing nothing is the right answer to an unfamiliar
screen.

Prefer `cmux rate-limit-options`. Arming Claude Code's own auto-resume ahead of
time means the menu never blocks in the first place, with no polling and no
screen scraping. `watch` is the net for sessions that hit the limit unarmed, and
it is the only cmux command that keeps running.

The wording it looks for is one list in `internal/menu/limit.go`, so when Claude
Code rewords the menu, that is the file to change.

## Reconnecting an MCP server

`cmux mcp reconnect <server>` is the one command that does more than type. It
opens `/mcp`, walks the menu to the server you named, presses Reconnect, and
then reads the answer back off the screen, so it exits non-zero when the
reconnect actually failed rather than when a key was pressed.

Reading the screen instead of assuming is not fussiness. Three things move:

- **The action number.** A broken server shows `1. Reconnect`; a healthy one
  shows `3. Reconnect`, behind View tools and Clear authentication. Pressing a
  hardcoded `1` on a healthy server opens View tools and looks like it worked.
- **Which row the cursor is on.** The server list is not number-selectable, so
  the cursor is stepped onto the row — counted in entries, not screen lines,
  because a scope heading between groups is a line the cursor skips.
- **What else is on screen.** Claude Code echoes every prompt as `❯ /mcp`, the
  same character the menu cursor uses, and past results stay in the transcript.
  Only the block between the menu heading and its keyboard footer is read.

Names are matched exactly, because they nest: `agent-dashboard` is a prefix of
`agent-dashboard-channel`, and a substring match reconnects the wrong server —
or reports the wrong one's result.

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
