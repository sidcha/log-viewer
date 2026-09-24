# Internals: how lv stays small

## How it stays small

| | |
|---|---|
| Line index | A checkpoint every 1024 lines or every MiB, whichever comes first, and the table is **capped**: past 2¹⁹ entries of 16 bytes every second one is dropped and the strides double, so index memory is O(1) in file size. Seeking to a line means jumping to the nearest checkpoint and scanning forward a stride. |
| Filtered view | The same table over *matching* lines — a checkpoint every 1024 matches, and every 256 KiB whatever matched — so a filter that keeps a billion lines costs the same ceiling, and reaching the next of a handful of rare matches reads at most one stride of bytes, not the gigabytes between them. The positions of the lines on screen are remembered, so a redraw or a one-line scroll costs no scan at all. |
| Changing rules | A change that can only hide lines — a `never` rule added, an `only` rule dropped — rescans only where the old filter found something, skipping every stretch it had already ruled out. Adding a highlight rescans nothing. |
| File bytes | Read with `pread(2)` and cached by the kernel, which reclaims under pressure for free. `lv` keeps no copy of its own. On Linux, full-file passes hand ranges back with `posix_fadvise(DONTNEED)` so scanning a huge log does not evict everything else on the machine. |
| Matching | Literal patterns use a vectorised substring search. A regex runs on RE2, in linear time, unless it needs lookaround or backreferences. |
| Rendering | Only visible lines are laid out and highlighted, so frame cost is independent of file size; a line's syntax colouring is remembered while it stays on screen. Background progress asks for at most sixty frames a second, however fast the scan publishes. |
| Idle | `GOGC=50`, and `debug.FreeOSMemory()` once the UI has been quiet (`idle-release`), so freed memory goes back to the OS. |

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
`make scale`. These figures were taken before checkpoints gained a byte axis and
their own line numbers, which doubles an index entry to 16 bytes — about 1.5 MiB
for those 99,821 checkpoints — and has not been re-measured on that machine.
`make scale` holds the resident set to its budget on any machine.

### Following

Change notification comes from the kernel — inotify on Linux, kqueue on macOS
and the BSDs, `ReadDirectoryChangesW` on Windows — so an appended line shows up
in microseconds and an idle viewer wakes up never. A slow poll runs alongside as a backstop, because
kernel notifications are not universally reliable: they do not work on NFS or
CIFS, some FUSE filesystems drop them, and inotify watches can be exhausted.
Missing an append is a visible bug in a log viewer, so the poll guarantees
liveness while the kernel supplies the latency.

The parent directory is watched too. When a log is rotated the file watch stays
bound to the old inode, and the replacement is only visible as a create in the
directory; `lv` re-binds and re-indexes.
