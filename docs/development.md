# Development

## Build

Everything runs inside the nix dev shell; `make` enters it for you.

```sh
make build      # ./bin/lv
make test
make check      # fmt, vet, cross-builds, tests, release tooling
make release    # nix build .#lv
```

With direnv, `direnv allow` puts the toolchain on your path directly.

## Platforms

`lv` builds and runs on Linux, macOS, FreeBSD, OpenBSD and Windows; `make
cross` builds and vets every one of them, and `make check` runs it. Two things
are Linux-only: stopping the program piping into `lv` on exit needs `/proc` to
find it, and handing scanned pages back to the kernel needs `posix_fadvise`.
Asking the terminal for its colours needs a Unix tty; on Windows `COLORFGBG` or
the `background` setting decides.

## Testing

```sh
make test                                  # unit and TUI tests
make cross                                 # build and vet every platform
make race                                  # needs a 48-bit VMA kernel
make scale LV_SCALE_FILE=/path/to/huge.log # correctness and RSS at scale
```

The scale target cross-checks line counts and filter results against an
independent scan and asserts peak RSS stays inside its budget.

## Releasing

A release is a signed tag. `scripts/release.sh` makes it and never pushes;
pushing the tag to GitHub is what publishes it. The code lives on the Gitea
`origin` and is mirrored by pushing to a second remote, once:

```sh
git remote add github git@github.com:sidcha/log-viewer.git
```

The GitHub repository must be public, because the Go module proxy fetches
from it anonymously.

```sh
scripts/release.sh prepare            # v0.1.0 -> v0.1.1; --minor, --major
$EDITOR changelog/v0.1.1.md           # write it; delete every lv-release: line
scripts/release.sh publish            # make check, commit, git tag -s v0.1.1
git push origin master v0.1.1
git push github master v0.1.1
```

`prepare` bumps `version` in `flake.nix` and drafts the changelog with the
commits since the last tag as hints. `publish` refuses a changelog that still
has its `lv-release:` lines, a tree with changes besides those two files, and
a tag that already exists. It then commits both, signs the tag with the
changelog as its message, and checks the signature.

On GitHub, `.github/workflows/release.yml` runs for the tag. It refuses to
publish unless `scripts/verify-release-tag.sh` finds a good signature
certified by the release key (`D8861B9C6B9C4284D980D6016804DFEE281234B2`,
imported from `github.com/sidcha.gpg`), and unless `flake.nix` names the same
version. It then runs `make check` and `make dist` with a read-only token. A
separate job, the only one that can write to the repository, attaches the
archives and `SHA256SUMS` to a GitHub release and asks `proxy.golang.org` for
the version, after which

```sh
go install github.com/sidcha/log-viewer/cmd/lv@v0.1.1
```

works and pkg.go.dev lists it. A failed run can be retried from the Actions
tab with the tag as input; a release that already exists is updated in place.

A version 2.0.0 or later needs `/v2` at the end of the module path in `go.mod`
first — Go's rule for major versions, not lv's.
