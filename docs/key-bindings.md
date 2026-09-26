# Key bindings

These are the **defaults**. Every key in every mode — the log, the match
window, the prompt, the panels, the questions — is a named command bound in a
table, and any of them can be rebound, unbound or given a second key in your
own config:

```conf
bind <C-d>  half-page-down
unbind q
bind -T visual y  yank
```

[Configuration](configuration.md#configuration) explains the tables and the
key notation, `lv --dump-config` lists every binding and command, and
<kbd>F1</kbd> inside `lv` shows the map in force.

The default map is vi's, so most of it should already be in your fingers.

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
| `Esc` `q` | leave visual mode |

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
| `C-g` | file, position, size and syntax profile, like vi's |
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
