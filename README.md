# lv

A terminal log viewer for files that do not fit in memory, with vi's key map.

`lv` follows a growing log, marks what you care about, hides the lines you do
not, and writes the result back out. It is built for the case where the file is
100 GB and the machine has 10 GB: resident memory is a function of your terminal
size, not of the log.

```sh
lv /var/log/syslog          # open a file
lv -f /var/log/kern.log     # follow it, like tail -f
git log | lv                # read a pipe, from the top, like a pager
journalctl -f | lv -f       # follow one
qemu-system-x86_64 … 2>&1 | lv -f -r boot.json
lv -o ERROR -N healthcheck app.log
lv -r incident.json app.log  # reuse rules saved earlier
```

Press <kbd>F1</kbd> inside `lv` for the key bindings.

## Build

Everything runs inside the nix dev shell; `make` enters it for you.

```sh
make build      # ./bin/lv
make test
make check      # fmt, vet, test
make release    # nix build .#lv
```

With direnv, `direnv allow` puts the toolchain on your path directly.

## Keys

The default map is vi's, so most of it should already be in your fingers.
Everything can be replaced by name in your own config.

### Motions

Motions move a **cursor** — a real one, drawn at the character it is on, not
just a tinted row. The window follows only when the cursor would leave it. Type
a count before any of them to repeat it — `5j`, `12k`, `42G`.

`w`, `b` and `e` run on past the end of a line, as they do in vi; `h` and `l`
stay put, matching vi's default. With a filter on they follow what is on
screen, so the next word is on the next *visible* line.

| | |
|---|---|
| `h` `j` `k` `l` | left, down, up, right |
| `w` `b` `e` | word forward, word back, end of word — these cross lines |
| `0` `^` `$` | start of line, first non-blank, end of line |
| `gg` `G` | first line, last line (`42G` for line 42) |
| `H` `M` `L` | top, middle, bottom of the window |
| `C-e` `C-y` | scroll the window down / up |
| `C-d` `C-u` | half page down / up |
| `C-f` `C-b` `Space` | page down / up |
| `zz` `zt` `zb` | redraw with the cursor centred, at the top, at the bottom |
| `zl` `zh` | scroll the window right / left by `h-scroll-step` columns |

### Selecting and copying

| | |
|---|---|
| `v` `V` | character-wise / line-wise selection |
| `y` | copy the selection (while selecting) |
| `yy` | copy the current line; `3yy` for three |
| `Esc` | leave visual mode |

Copying uses the terminal's own clipboard escape, so it works over ssh and
inside a container where there is no X display or Wayland socket to reach. If
your terminal has that turned off, set `clipboard-command` to `wl-copy`,
`pbcopy` or similar. A yank out of a filtered view copies what is on screen, not
what is underneath it.

`y` is only a command once something is selected; otherwise it is the first half
of `yy`, exactly as in vim. Visual mode has its own binding table, so either can
be rebound independently.

### Searching and rules

| | |
|---|---|
| `f` | fuzzy-find a line and go to it, leaving nothing behind |
| `/` `?` | search forward / back — lists matches as you type |
| `n` `N` | next / previous match |
| `C-l` | drop the search highlight and repaint |
| `*` `#` | search for the word under the cursor (move to it with `w`/`b`/`l`) |
| `&` `!` `m` | add an only / never / highlight rule |
| `C-r` | in any of those, read the pattern as PCRE instead |
| `=` | the rule list: edit, enable, disable, delete |
| `\` | disable every rule for a moment |
| `C-p` `C-x` `zm` | remove the last rule, every rule, every highlight rule |

### View and other

| | |
|---|---|
| `F` | follow the end of the file |
| `zw` `zn` `zs` | wrap, line numbers, pin the status line |
| `zr` | re-index |
| `S` `zS` | save the current view / the rules to a file |
| `:` | command line |
| `F1` | help |
| `q` `ZZ` | quit, offering to save the rules |
| `ZQ` | quit without asking |

### The command line

`:` works the way vi's does, including the aliases worth having:

```
:42               go to line 42
:f timeout        fuzzy-find a line and go to it
:$                go to the end
:set wrap         toggle a setting; :set tab-width 4 to assign one
:only ERROR       add a rule
:never healthz
:hl re:OOM|oom-killer   re: reads the pattern as a regular expression
:noh              drop the search highlight (leaves your rules alone)
:w out.log        write the current view
:mks rules.json   write the rules
:source rules.json  load rules from a file
:q                quit, offering to save the rules
:q!               quit without asking
```

Tab completes command names. Every key binding is a named command, so anything
a key does can be typed here, and anything typed here can be bound.

## Rules

Filtering and marking are the same thing with three actions:

| | | |
|---|---|---|
| `only` | `&` | show only lines matching the pattern |
| `never` | `!` | never show lines matching the pattern |
| `highlight` | `m` | mark matches, without changing which lines are shown |

Rules form one flat list, evaluated in a single pass over the file. A line is
shown when it matches at least one `only` rule (or there are none) and matches
no `never` rule — that is, `grep -e … -e … | grep -v …`.

**`only` rules are or-ed.** Press `&` as many times as you like: each pattern
widens what is shown rather than replacing the last, so `&ERROR` then `&WARN`
gives you both. `never` rules are and-ed, and always narrow. `=` spells out how
the current set combines.

While you type a further `only` pattern the window searches the whole log rather
than what is left of it, because the lines such a rule would bring back are by
definition the ones currently hidden. Never rules still apply there: lines
banished on purpose do not come back because a new pattern happens to match
them.

A search highlight is a separate, transient thing: `/` lights up every match,
`C-l` or `:noh` drops it, and neither touches the rules you built. The reverse
holds too — clearing your highlight rules leaves the search for `n` and `N`.

Every rule gets a colour so you can see which one caught which line. Colours are
picked at random but nudged apart: several candidate hues are drawn and the one
furthest from the colours already on screen wins, so half a dozen highlights
stay distinguishable instead of colliding the way plain random assignment does.

### Managing them

`=` opens the rule list. It is the place to change your mind about a filter
without rebuilding it:

| | |
|---|---|
| `Space` `Enter` | enable / disable the rule |
| `e` | edit its pattern in the match window |
| `a` | cycle it between only, never and highlight |
| `r` | give it a different colour |
| `d` | delete it |
| `A` `D` | enable / disable every rule |

Disabling is not deleting: `\` turns everything off for a glance at the raw log
and back on again, and a disabled rule keeps its place, its colour and its
pattern.

### Keeping them

Working out the right filters over an unfamiliar log takes real effort, and that
effort outlives the session. Rules save to a JSON file:

```sh
lv -r incident.json /var/log/app.log    # start from a saved set
```

Quitting with rules that are not on disk asks first — offering to update the
file they came from, or to write a new one. It stays quiet when there is nothing
to lose, or when nothing has changed since the file was read, so opening a log
and just looking at it is untouched. `ZQ` and `:q!` skip the question.

The format is plain enough to write by hand and to review in a diff; everything
but the action and the pattern is optional:

```json
{
  "version": 1,
  "case": "smart",
  "rules": [
    { "action": "only",      "pattern": "ERROR", "color": "#8decc8" },
    { "action": "never",     "pattern": "GET /healthz", "disabled": true },
    { "action": "highlight", "pattern": "OOM|oom-killer", "match": "regex" }
  ]
}
```

`match` is `literal` when absent, and how the pattern is read is saved with it:
a rule written as plain text does not start interpreting its own punctuation
because it was reloaded.

A search is not part of this. It hides nothing and is replaced by the next
pattern typed, so it never counts as unsaved work: searching and then quitting
asks nothing.

The case mode is recorded because it is part of what the rules mean: a set built
under `sensitive` would quietly widen if it were reloaded somewhere configured
for smart case.

### The match window

`f`, `/`, `?`, `&`, `!` and `m` open a window across the bottom of the screen
that lists the lines the pattern actually matches, updated as you type. The log
above is left exactly as it was: nothing is filtered, dimmed or moved until you
press <kbd>Enter</kbd>, so the window can be opened, typed into and abandoned
without losing your place.

| | |
|---|---|
| <kbd>↑</kbd> <kbd>↓</kbd> | pick a match |
| <kbd>Tab</kbd> | go to it, leaving the window open |
| <kbd>Enter</kbd> | apply the rule, or for `/` jump to the highlighted match |
| <kbd>Esc</kbd> | cancel, changing nothing |
| <kbd>C-r</kbd> | read the pattern as a regular expression, and back |
| <kbd>C-p</kbd> <kbd>C-n</kbd> | recall patterns typed earlier |

The search starts from where you are reading and runs down the file, stopping as
soon as there are enough matches to fill the window — so it costs a few hundred
kilobytes, not a pass over the log, however large the file is. For `!` the listed
lines are the casualties, and are struck out to say so. For `&` the search
deliberately looks past the filter in force: a further `only` rule is a question
about lines that are currently hidden, which is precisely what an inline preview
could never show. Rules already in force are summarised in the window's frame,
and `=` opens the full list.

`f`, `/` and `?` carry on from the top of the file when the matches below you
run out, and the frame says `wrapped` once they have. A window that reported no
matches while the pattern sat ten lines above — and then lit those matches up
the moment it was committed — would be worse than no window at all. `n` and `N`
wrap for the same reason, saying so when they do, so they can reach everything
the window listed. The rule windows do not wrap: a rule applies to the whole
file however it was found, so the matches below are a fair sample of it.

### Going somewhere

`f` is the same window used for nothing but navigation, the way fzf picks a
file: type a fragment of the line you are after, pick it, and <kbd>Enter</kbd>
moves the cursor there. It leaves *nothing* behind — no highlight to clear
afterwards, and `n` and `N` carry on stepping through whatever you last searched
for.

Like `/`, it fills the window from below the cursor first and then carries on
from the start of the file, stopping where it began — the line you are reaching
for is as likely to be behind you as ahead.

An unbalanced bracket says so while you type rather than after you commit. Over
a very large log a filter is slow to undo, and a pattern that looked right often
is not.

### How a pattern is read

Three modes, chosen explicitly rather than guessed from the spelling:

| | |
|---|---|
| **literal** | plain text; the default everywhere but `f` |
| **fuzzy** | the characters in order with anything between; the default for `f` |
| **regex** | [PCRE][pcre] — lookaround, backreferences, lazy quantifiers |

A pattern means the characters in it. A filter is saved, shared and reread
months later, so it had better say what it means — `a.b` matches `a.b`, not
`axb` — and a search is no different: `/` finds what you typed, not everything
with those letters scattered through it.

`f` is the exception, because loose matching is the entire point of it: you are
reaching for a line from a half-remembered fragment, and `f` `erbd` should land
on `ERROR: bad descriptor`. <kbd>C-r</kbd> switches any of them to PCRE, which
the window title shows, and the mode travels with the rule into the rules file.

Because PCRE backtracks, a single match is bounded by a timeout: a pathological
pattern over a hostile line stops rather than stalling the scan. Case follows
`set case`, which defaults to `smart`: insensitive unless you type a capital
letter.

[pcre]: https://www.pcre.org/current/doc/html/pcre2pattern.html

## Configuration

Files cascade the way git's do. All of these are read, in order, each overriding
the last:

```
/etc/lv/config           --system
~/.config/lv/config      --global    ($XDG_CONFIG_HOME/lv/config if set)
./.lvconfig              --local
$LV_CONFIG               if set
```

Read and write them with `lv config`:

```sh
lv config --list                  # every setting and its value
lv config --list --show-origin    # and which file set it
lv config tab-width               # print one value
lv config tab-width 4             # set it (--global by default)
lv config --local wrap on         # set it for this directory only
lv config --unset tab-width
lv config --edit                  # open it in $EDITOR
```

`--edit` refuses to leave behind a file `lv` would reject at startup.

The file syntax is tmux-flavoured: one directive per line, `#` comments,
`'single'` quotes that pass text through untouched (use these for regexes),
`"double"` quotes that interpret `\n \t \\ \"`, a trailing `\` to continue, and
`{ }` blocks.

```conf
set follow on
set case sensitive
set scroll-off 5
set cursor-line off
set background dark
set clipboard-command wl-copy

# Key sequences are written as vim writes them.
bind gg        goto-start
bind <C-d>     half-page-down
unbind q
bind ZQ        quit
bind -T visual y        yank
bind -T finder <C-t>    finder-toggle-regex
bind -T confirm <Enter> confirm-yes

rule never     'GET /healthz'
rule highlight 'OOM|oom-killer' match=regex color=#ff5555

style syntax.level.error fg=#ff0000 bold
style cursor.line        bg=#303030

# Colours that only suit one kind of terminal go in a background block.
background light {
    style cursor.line bg=#eaeef2
}

profile myapp {
    match '*.applog'
    detect '^\[[0-9]+\] [A-Z]+ '
    rule '^\[[0-9]+\]'      syntax.time
    rule '\b(WARN|ERROR)\b' syntax.level.error
}
```

Every key lv answers to is bound in a table named for the mode it works in,
so every one of them can be rebound. `bind` without `-T` means `normal`, the
log itself. A key a table leaves unbound falls through to the one it builds on:

| table | when | falls back to |
|---|---|---|
| `normal` | reading the log | |
| `visual` | while selecting | `normal` |
| `line` | editing a line of text | |
| `prompt` | at the `:` and path prompts | `line` |
| `finder` | in the match window | `line` |
| `overlay` | in a panel: help, rules | |
| `rules` | in the rule list | `overlay` |
| `confirm` | answering a question | |

Each table takes the commands that make sense in its mode, and `lv
--dump-config` lists them all. `bind-visual` is kept as another way to write
`bind -T visual`.

A binding is one key (`C-a`, `PgUp`), a short bare sequence (`gg`, `zz`), or vim
notation where anything in angle brackets is a single key (`<C-w>s`). A longer
bare run is rejected rather than silently bound as a sequence, so a misspelled
key name tells you about itself.

`C-` is Control, `M-` is Alt and `S-` is Shift — so `S-PgUp` and `C-Up` bind as
you would expect. The mouse wheel is two more keys, `<WheelUp>` and
`<WheelDown>`, bound in each table like the rest: `bind <WheelDown> scroll-down
5`. No default binding uses Alt: a key you have to deduce is a key
nobody presses. lv's own commands sit under `z`, leaving the vi keys alone.

`S-Space` is the one thing that cannot be bound. Shift does not change the space
character, so the modifier is the only thing that could distinguish the two, and
the terminal library drops it from rune events before lv sees them. `b`, `C-b`
and `PgUp` page back instead, in the log and in any panel.

Because `#` starts a comment when it begins a word, a `#` or `{` inside a key
specification or a pattern must be quoted: `bind '#' search-word-back`.

Unknown settings, commands, styles and keys are errors at startup rather than
silent no-ops. `lv --dump-config` prints the merged result, re-parseable as-is.

## Terminal colours

lv has to look right on a black terminal, a white one, and whatever gruvbox or
solarized variant you actually use. Absolute colours cannot do that: a cursor
line of `#1c1c1c` is a clear band on a black terminal, invisible on one whose
background happens to be `#1c1c1c`, and a dark smear on a light one.

So lv asks. On startup it queries the terminal for its real foreground and
background (OSC 10 and OSC 11) and derives the background-relative tints — the
cursor line among them — by moving a fixed distance from the background towards
the foreground. Those then sit correctly against any theme, without lv knowing
which one it is.

The rest of the palette is a pair of themes, chosen by whether the background
came back dark or light. When the terminal will not answer, `COLORFGBG` decides;
failing that, lv assumes dark.

```sh
lv --background light app.log     # skip detection
lv config background dark         # or settle it permanently
```

Anything you set yourself wins: a `style cursor.line …` in your config is never
replaced by a derived colour. To see what was resolved:

```sh
lv --background light --dump-config | grep '^style'
```

## Syntax highlighting

`lv` recognises *log* formats, not programming languages. It ships with profiles
for `kernel` (dmesg/`kern.log`), `syslog`, `iso` (ISO-8601 timestamped
application logs), `json` line-delimited records, and a `generic` fallback. The
profile is chosen by filename glob, then by sniffing the head of the file;
`-p NAME` forces one and `-p none` turns it off.

### Logs that bring their own colours

A log captured from a serial console, a build, or anything else that writes to
a tty arrives with its escape sequences still in it. Those bytes are not text:
drawn literally they are noise, and counted as columns they push the whole line
out of alignment — so every escape sequence `lv` recognises takes no columns at
all, whether it is colour, a cursor move, or a window title.

The colour ones are put back as colour, so the file looks the way it did when
it was produced. Four layers decide a character's appearance, each overriding
the one before:

1. `lv`'s syntax profile — its guess at the shape of the line;
2. the colours the line was written with;
3. the rules you added;
4. the search.

So a log that is painted end to end does not hide what you are hunting for, and
a line the producer coloured only in part keeps `lv`'s timestamp colouring
across the rest. A sequence that asks only for bold has said nothing about
colour, so it adds to what is there rather than replacing it.

`lv config ansi off` goes back to ignoring them. They stay hidden either way —
they are not text under any setting.

Two limits worth knowing. Colour state does not carry across a line break:
jumping into the middle of a 100 GB file would otherwise mean reading
everything above it to find out what colour the line starts in. And a carriage
return is dropped only at the end of a line, where it is the line ending;
inside a line it stays visible as `·` rather than being emulated, so text that
never appeared together is not silently run together.

## How it stays small

| | |
|---|---|
| Line index | Every 1024th line's offset only, and the table is **capped**: past 2²⁰ entries every second one is dropped and the stride doubles, so index memory is O(1) in file size. Seeking to a line means jumping to the nearest checkpoint and scanning forward one block. |
| Filtered view | The same sparse trick over *matching* lines, so a filter that keeps a billion lines costs the same ceiling. |
| File bytes | Read with `pread(2)` and cached by the kernel, which reclaims under pressure for free. `lv` keeps no copy of its own. Full-file passes hand ranges back with `posix_fadvise(DONTNEED)` so scanning a huge log does not evict everything else on the machine. |
| Rendering | Only visible lines are laid out and highlighted, so frame cost is independent of file size. Buffers are reused, so drawing allocates nothing. |
| Idle | `GOGC=50`, and `debug.FreeOSMemory()` once the UI has been quiet, so freed memory goes back to the OS. |

Indexing, filtering, searching and saving run in the background, publish partial
results as they go, and are cancellable with <kbd>Esc</kbd>. The first screen is
drawn as soon as the first block is indexed, not when the file has been read.
Adding a highlight does not disturb a filter scan already in progress.

### Measured

A 10 GiB / 102,216,113-line log on a Raspberry Pi 4 with 3.9 GiB of RAM — so the
file is 2.7× the machine's total memory — reading from SD at ~94 MB/s:

| | |
|---|---|
| Time to a usable first screen | immediate; the line count climbs while you read |
| Full index | 1 m 56 s, 88 MiB/s — disk-bound, not CPU-bound |
| Index size for 102 M lines | **779 KiB** (99,821 checkpoints). A dense table would be 818 MB |
| Filtering to 14,602,301 matches | 1 m 55 s, 89 MiB/s, streaming |
| **Peak RSS, interactive session** | **16.6 MiB** while 17.2 GiB was read through a concurrent index and filter |

Line counts and filter results were checked against an independent scan; see
`make scale`.

### Following

Change notification comes from the kernel — inotify, kqueue,
`ReadDirectoryChangesW` — so an appended line shows up in microseconds and an
idle viewer wakes up never. A slow poll runs alongside as a backstop, because
kernel notifications are not universally reliable: they do not work on NFS or
CIFS, some FUSE filesystems drop them, and inotify watches can be exhausted.
Missing an append is a visible bug in a log viewer, so the poll guarantees
liveness while the kernel supplies the latency.

The parent directory is watched too. When a log is rotated the file watch stays
bound to the old inode, and the replacement is only visible as a create in the
directory; `lv` re-binds and re-indexes.

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
drawn as escape sequences (see [above](#logs-that-bring-their-own-colours)).

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

## Testing

```sh
make test                                  # unit and TUI tests
make race                                  # needs a 48-bit VMA kernel
make scale LV_SCALE_FILE=/path/to/huge.log # correctness and RSS at scale
```

The scale target cross-checks line counts and filter results against an
independent scan and asserts peak RSS stays inside its budget.

## Licence

Apache-2.0. See [LICENSE](LICENSE).
