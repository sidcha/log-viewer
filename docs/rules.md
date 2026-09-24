# Rules: filtering and highlighting

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
