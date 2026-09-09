# How cmux types into a Claude Code pane

Findings from the spike in issue #1, measured against Claude Code v2.1.266 in tmux 3.4.

## The method

```sh
printf '%s' "/compact " | tmux load-buffer -
tmux paste-buffer -t "$TARGET"
tmux send-keys -t "$TARGET" Enter
```

Three calls: load the text into a tmux buffer, paste it into the target pane, then send Enter
as a separate key.

Two details matter.

**The trailing space is required.** Typing `/` opens Claude Code's slash-command menu, and
pasting does not avoid it — the menu opens for a pasted `/compact` exactly as it does for a
typed one. With the menu open, Enter selects the highlighted entry rather than submitting what
was typed. A trailing space closes the menu, so Enter submits the literal text. Without it,
`/status` happens to work because `/status` is the first entry in its own menu, but a command
that is a prefix of another would fire the wrong one.

**The Enter must be a separate `send-keys`.** Putting a newline inside the pasted payload looks
tempting — one atomic operation, no race — and it does work for short text. It fails for
anything long enough to wrap onto a second display row: the newline is inserted as a literal
newline and the text just sits in the box. Measured at every delay from 0s to 1s, paste
followed by a separate Enter submitted correctly for both short and long payloads.

No delay is needed between the paste and the Enter when the target is idle.

## What survives the trip

Quotes, em-dashes, accented characters, emoji, `$HOME` and backticks all arrive literally.
tmux buffers carry bytes, so there is no shell expansion and nothing to escape.

Newlines do not survive usefully. In a short payload a newline submits early, truncating the
command; in a wrapping payload it becomes a literal newline. cmux therefore collapses any
newline in its arguments to a single space before sending.

## Timing: slash commands do not queue

This is the finding that changed the design.

A **plain text** message injected while the target is mid-turn lands in the input box and waits
there until the turn finishes. That is the queuing behaviour cmux was designed around.

A **slash command** does not wait. `/cost`, `/status` and `/clear` all executed immediately when
injected into a pane that had a shell command still running.

### The hazard

`/clear` was injected into a pane running a 30-second shell command. It cleared the transcript
straight away, while that shell was still running — the session's history was destroyed
mid-tool-call.

This matters because of how cmux is meant to be used. When Claude runs `cmux compact` from a
Bash tool call, its own turn is by definition still in flight. So a self-targeted `compact` or
`clear` fires in exactly the situation that caused the damage above.

cmux does not try to solve this, because there is no reliable way to defer a slash command
without a daemon. It does two things instead: it prints a warning when `clear` or `compact` is
aimed at the calling pane, and `--defer <seconds>` schedules the injection through
`tmux run-shell -b -d` for callers who want to get their turn out of the way first.

### Repeated injections concatenate

If a first injection is still sitting unsent in the input box, a second one appends to it and
both go out as one garbled message. Only possible with queued plain text, not with slash
commands, but it is why cmux sends one command per invocation and never batches.

## Escape

`Escape` sent on its own works as a raw key: it dismisses dialogs and cancels a running turn.
It takes effect immediately and never queues, which is why `interrupt` refuses to target its own
pane without `--self`.
