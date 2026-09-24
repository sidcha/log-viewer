# lv

[![CI](https://github.com/sidcha/log-viewer/actions/workflows/ci.yml/badge.svg)](https://github.com/sidcha/log-viewer/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/sidcha/log-viewer?sort=semver)](https://github.com/sidcha/log-viewer/releases/latest)
[![Go Reference](https://pkg.go.dev/badge/github.com/sidcha/log-viewer.svg)](https://pkg.go.dev/github.com/sidcha/log-viewer)
[![Go version](https://img.shields.io/github/go-mod/go-version/sidcha/log-viewer)](go.mod)
[![Licence](https://img.shields.io/github/license/sidcha/log-viewer)](LICENSE)

**A terminal log viewer for files bigger than your RAM, with vi's keys.**

Open a 100 GB log on a machine with 10 GB and it starts instantly and stays
small: memory depends on your terminal size, not on the file.

- **Huge files, flat memory.** Indexing runs in the background; the first
  screen appears at once.
- **vi keys.** `j`/`k`, `/`, `n`, `gg`/`G`, counts, visual mode — and every key
  can be rebound.
- **Filters that stack.** Show only what matches, hide what doesn't, highlight
  the rest; save the set and reuse it on the next incident.
- **Knows log formats.** Kernel, syslog, JSON and ISO-timestamped logs are
  recognised from their content and coloured; add your own in a few lines.
- **Follows and pages.** `tail -f` for growing files and pipes, and a drop-in
  `$PAGER` for `git log` and friends.

Linux, macOS, FreeBSD, OpenBSD and Windows.

## Install

```sh
go install github.com/sidcha/log-viewer/cmd/lv@latest
```

Or download a binary from the [releases page](https://github.com/sidcha/log-viewer/releases),
run it with Nix (`nix run github:sidcha/log-viewer`), or build from source
with `make build` (see [development](docs/development.md)).

## Quick start

```sh
lv /var/log/syslog              # open a file
lv -f /var/log/kern.log         # follow it, like tail -f
journalctl -f | lv -f           # follow a pipe
git log | lv                    # use it as a pager
lv -o ERROR -N healthcheck app.log   # only ERROR lines, never healthcheck
lv -r incident.json app.log     # reuse rules saved earlier
```

Inside `lv`:

| Key | Does |
|---|---|
| `j` `k` `gg` `G` | move down, up, to the top, to the end |
| `C-d` `C-u` `Space` | half page down / up, page down |
| `/` `?` `n` `N` | search forward, back, next, previous |
| `f` | fuzzy-find a line and jump to it |
| `&` `!` `m` | show only / never show / highlight matching lines |
| `=` | list, edit and toggle your rules |
| `\` | turn every rule off for a moment |
| `F` | follow the end of the file |
| `:` | command line (`:42`, `:set wrap`, `:w out.log`) |
| `F1` | help |
| `q` | quit |

These are defaults — see [key bindings](docs/key-bindings.md) for the full map
and how to change any of it.

## Documentation

- [Key bindings](docs/key-bindings.md) — every key, and how to rebind them
- [Rules](docs/rules.md) — filtering, highlighting, saving and reusing rules
- [Configuration](docs/configuration.md) — settings, config files, colours
- [Syntax highlighting](docs/highlighting.md) — log formats and your own profiles
- [tmux, pipes and paging](docs/pager-and-tmux.md) — captures, `$PAGER`, tmux
- [Internals](docs/internals.md) — how it stays small, with measurements
- [Development](docs/development.md) — building, testing and releasing

## Licence

Apache-2.0. See [LICENSE](LICENSE).
