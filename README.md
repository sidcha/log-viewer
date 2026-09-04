# lv

A terminal log viewer for files that do not fit in memory.

`lv` follows a growing log, marks what you care about, hides the lines you do
not, and writes the result back out. It is built for the case where the file is
100 GB and the machine has 10 GB: resident memory is a function of your terminal
size, not of the log.

```sh
lv /var/log/syslog          # open a file
lv -f /var/log/kern.log     # follow it, like tail -f
journalctl -f | lv          # read a pipe
lv -o ERROR -N healthcheck app.log
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

## Rules

Filtering and marking are the same thing with three actions:

| | | |
|---|---|---|
| `only` | `&` | show only lines matching the pattern |
| `never` | `!` | never show lines matching the pattern |
| `highlight` | `*` | mark matches, without changing which lines are shown |

Rules form one flat list, evaluated in a single pass over the file. A line is
shown when it matches at least one `only` rule (or there are none) and matches
no `never` rule — that is, `grep -e … | grep -v …`.

Press `=` to see the list, `Space` to toggle a rule, `d` to delete one, `\` to
disable everything for a moment without losing what you built up.

Every rule gets a colour so you can see which one caught which line. Colours are
picked at random but nudged apart: several candidate hues are drawn and the one
furthest from the colours already on screen wins, so six highlights stay
distinguishable instead of colliding the way plain random assignment does.

### Preview

While you are typing a pattern, `lv` shows what it would do to the lines already
on screen — matching text painted in the colour the rule is about to take, lines
the rule would remove dimmed, and a count beside the prompt:

```
show only: ERROR    2 of 7 shown
```

An unbalanced bracket says so while you type rather than after you commit. Over
a very large log a filter is slow to undo, and a pattern that looked right often
is not.

Patterns are regular expressions ([RE2][re2], so no pathological backtracking).
A pattern with no metacharacters compiles to a plain substring search instead,
which is much faster over a large file. Case follows `set case`, which defaults
to `smart`: insensitive unless you type a capital letter.

[re2]: https://github.com/google/re2/wiki/Syntax

## Keys

All of these are configurable; this is the default map.

| | |
|---|---|
| `j` `k` `↑` `↓` | line up / down |
| `h` `l` `←` `→` `0` | scroll sideways, back to column 0 |
| `Space` `C-f` `PgDn` / `b` `C-b` `PgUp` | page down / up |
| `C-d` `C-u` | half page down / up |
| `g` `G` `Home` `End` | start / end |
| `L` | go to a line number |
| `/` `?` `n` `N` | search forward / back, next / previous |
| `&` `!` `*` | add an only / never / highlight rule |
| `=` `C-p` `C-x` `M-*` | list rules, remove the last, remove all, remove highlights |
| `\` | disable all rules for a moment |
| `F` | follow the end of the file |
| `w` `#` `s` | wrap, line numbers, pin the status line |
| `S` | save the current view to a file |
| `r` `C-r` | re-index, repaint |
| `:` | command prompt |
| `Esc` | close an overlay, or cancel a running search or save |
| `q` `C-c` | quit |

Anything a key can do, the `:` prompt can do too, with the same names — so
`:filter-never 'health ?check'` works, and so does binding it.

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

unbind q
bind C-q quit
bind M-e filter-never 'health ?check'

rule never  'GET /healthz'
rule highlight 'OOM|oom-killer' color=#ff5555

style syntax.level.error fg=#ff0000 bold

profile myapp {
    match '*.applog'
    detect '^\[[0-9]+\] [A-Z]+ '
    rule '^\[[0-9]+\]'      syntax.time
    rule '\b(WARN|ERROR)\b' syntax.level.error
}
```

Because `#` starts a comment when it begins a word, a `#` or `{` inside a key
specification or a pattern must be quoted: `bind '#' line-numbers-toggle`.

Unknown settings, commands, styles and keys are errors at startup rather than
silent no-ops, so a typo tells you about itself. `lv --dump-config` prints the
merged result, re-parseable as-is.

## Syntax highlighting

`lv` recognises *log* formats, not programming languages. It ships with profiles
for `kernel` (dmesg/`kern.log`), `syslog`, `iso` (ISO-8601 timestamped
application logs), `json` line-delimited records, and a `generic` fallback. The
profile is chosen by filename glob, then by sniffing the head of the file;
`-p NAME` forces one and `-p none` turns it off.

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

A pipe is spooled to an unlinked temporary file, so seeking, filtering and
saving work on piped input exactly as they do on a file, and nothing is left
behind when `lv` exits.

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
