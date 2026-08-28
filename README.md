# lv

A terminal log viewer for files that do not fit in memory.

`lv` follows a growing log, highlights what you care about, hides the lines you
do not, and writes the result back out. It is built for the case where the file
is 100 GB and the machine has 10 GB.

## Build

Everything runs inside the nix dev shell; `make` enters it for you.

```sh
make build      # ./bin/lv
make test
make release    # nix build .#lv
```

With direnv, `direnv allow` puts the toolchain on your path directly.

## Licence

Apache-2.0. See [LICENSE](LICENSE).
