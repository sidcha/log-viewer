# Configuration

## Configuration

Files cascade the way git's do. All of these are read, in order, each overriding
the last:

```
/etc/lv/config           --system
~/.config/lv/config      --global    ($XDG_CONFIG_HOME/lv/config if set)
./.lvconfig              --local
$LV_CONFIG               if set
```

On Windows the system file is `%ProgramData%\lv\config` and the per-user one
`%AppData%\lv\config`.

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
