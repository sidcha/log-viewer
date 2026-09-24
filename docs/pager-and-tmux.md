# tmux, pipes and paging

## tmux

`lv` is an ordinary full-screen program and behaves normally under tmux,
including resize and mouse wheel. Reading from a pipe works too — `lv` takes key
input from `/dev/tty`, not stdin, so `producer | lv` stays interactive.

```sh
tmux new-window -n logs 'lv -f /var/log/syslog'
tmux display-popup -E -w 90% -h 90% 'lv /var/log/kern.log'
```

For `y` to reach your clipboard through tmux, tmux needs `set -g set-clipboard
on`; otherwise point `clipboard-command` at an external program.

A pipe is spooled to an unlinked temporary file, so seeking, filtering and
saving work on piped input exactly as they do on a file, and nothing is left
behind when `lv` exits. Piped input opens at the top, the way a pager does;
`lv -f` follows the end instead, which is what you want for `qemu … | lv -f` or
`dmesg -w | lv -f`.

### Keeping a capture

Piped input exists nowhere but in that unlinked spool: when the process ends
the capture ends with it, and whatever produced it has long since moved on. So
quitting a session that was *following* the input asks about it:

```
Piped input is not on disk. Keep it?   (y) save all…  (n) discard  (c) cancel
```

Following is the signal: it is how you watch something happen — a boot, a
build, a device talking — and that is a capture. Reading through the output of
a command is not, and is left alone, because a pager that stopped to ask on the
way out of `git log` would be intolerable. Set `ask-save on` to be asked every
time, or `off` for never.

`y` writes **all** of the input, not the filtered view `:w` writes: the question
was about losing the input, and answering it with a subset would be a trap.
Saving the whole thing during the session, with `S` or `:w` and no filter in
force, settles the question in advance.

The rules are a separate question, with its own setting:

```sh
lv config ask-save-rules off    # never ask about the rules either
```

`ZQ` and `:q!` skip both regardless.

## As a pager

```sh
git config --global core.pager lv
export PAGER=lv MANPAGER=lv
```

Anything that pipes its output to a configured pager works, because reading a
pipe is what `lv` does anyway. Three things follow from being on the other end
of one:

**It opens at the top and keeps the colours.** Whatever the producer coloured
stays coloured — git's diffs and decorations arrive as escape sequences and are
drawn as escape sequences (see [Logs that bring their own colours](highlighting.md#logs-that-bring-their-own-colours)).

**Short output is printed, not paged.** `git log --oneline -3` prints three
lines and gives the prompt back rather than taking the terminal over to show
them. That is what `less -F` does, and why git asks for `LESS=FRX`. `lv config
quit-if-one-screen off` turns it off; it is off by default for a file you named
yourself, which you opened on purpose.

**What you read can stay on the screen.** By default `lv` uses the alternate
screen and leaves the terminal as it found it. For a pager that is often wrong:
the point of paging `git log` is usually to read a hash and then use it, and a
pager that wipes it on `q` makes you run the command again. `less` calls this
`-X`:

```sh
lv config alt-screen off
```

Nothing here needs a flag in `core.pager`, since `lv`'s own configuration
cascades the same way git's does. If you would rather keep it in one place,
`core.pager = "lv --set alt-screen=off"` works too.
