# Syntax highlighting

## Syntax highlighting

`lv` recognises *log* formats, not programming languages. It ships with profiles
for `kernel` (dmesg/`kern.log`), `syslog`, `iso` (ISO-8601 timestamped
application logs), `json` line-delimited records, and a `generic` fallback.

The profile is chosen from what the log contains: lv samples its first 64
lines and picks the profile whose `detect` patterns match the largest share,
if that is at least 30%. A filename glob adds to a profile's score but never
decides alone, so a kernel log called `app.log` is still a kernel log; only a
profile with no `detect` patterns is chosen by its glob. `-p NAME` forces one
and `-p none` turns it off; the help overlay (`F1`) says which profile the
line under the cursor has and why.

A capture from a serial console passes through several programs — firmware,
a bootloader, a kernel — and each can be recognised in turn. A `marker` line
starts a new stretch of the log: inside a profile it names that profile,
at the top level it says only that something new begins, and the next 64
lines decide what. Markers are configured like profiles and lv ships none of
its own. With the example below, a device that reboots while you follow it
switches to its bootloader's colours on the banner, since markers are found
while the file is indexed.

```conf
marker 'Starting kernel'
profile uboot {
    marker 'U-Boot ' '^U-Boot [0-9]{4}\.[0-9]{2}'
    rule '^=> ' syntax.level.warn
}
```

A stretch is decided once it has 64 lines, or `detect-wait` after its first
line if fewer have come. When a file is opened without `-f`, the part already
written is decided as soon as it has been read. `detect-lines`,
`detect-threshold` and `detect-wait` tune the sample, the share it must
reach, and the wait.

Every profile starts from `tokens common`, the tokens nearly every log shares:
timestamps, numbers with their units, IP and MAC addresses, UUIDs, paths and
URLs, `key=value`, quoted strings and stack-trace frames. `generic` is that set
and nothing else. Level words are coloured only where they cannot be prose — in
capitals (`ERROR`), or delimited (`[err]`, `<wrn>`, `level=warn`) — so a
message that mentions an error is not painted as one. A `tokens` block of your
own, used with `use`, shares rules between your profiles the same way:

```conf
tokens mine {
    rule 'req-[0-9a-f]+' syntax.uuid
}
profile myapp {
    use common
    use mine
}
```

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
