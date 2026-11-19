# goHide

Run a command and it hides itself as a kernel worker. No flags, no setup:

```sh
gohide nmap -sCV -F -Pn scanme.nmap.org
ps -ef | grep nmap     # -> [kworker/3:1-events]  (real options gone)
```

The ptrace engine (vendored from [thc/zapper](https://github.com/hackerschoice/zapper))
wipes argv and `/proc/PID/environ` right after `execve()`, so `ps`, `top`, and
`/proc` see only a fake kworker name. No `LD_PRELOAD` — static binaries work
too.

## Build

```sh
make            # bin/gohide + bin/gohide-zapper
make test
make smoke
```

`gohide` is a Go launcher. It uses `gohide-zapper` next to the binary, or
compiles the embedded engine into `~/.cache/gohide/` on first run (needs
`cc`/`gcc`/`clang`).

## Usage

```sh
gohide [-no-pt] <command> [args...]
gohide version
```

Comm spoofing is on by default: an injected `prctl(PR_SET_NAME)` makes plain
`top`/`htop` and `ps -o comm` show the fake name instead of the real
executable. Use `-no-pt` to turn it off.

Optional environment:

| var | effect |
| --- | --- |
| `GOHIDE_NAME=NAME` | custom name instead of a random kworker |
| `GOHIDE_PT=0` | same as `-no-pt` |
| `GOHIDE_FOLLOW=1` | hide child processes too |
| `GOHIDE_KEEP_ENV=1` | leave the environment alone |
| `GOHIDE_PID=N` | start under pid N |

## Notes

- With comm spoofing on, the kernel `comm` field (`ps -o comm`, default
  `top`/`htop`) and the full command line (`ps aux`, `ps -ef`) both show the
  fake name (comm is limited to 15 bytes). `-no-pt` leaves comm as the real
  executable name. `/proc/PID/exe` is unchanged regardless.
- The program itself still sees its real argv/env, so it keeps working.
- There's a short window between `execve()` and the ptrace stop where the
  options are visible. Pass secrets via env/stdin.
- A pure-Go version of the engine is being worked on in
  `experimental/go-engine/` — not used by the released binary yet.

## Attribution

`core/zapper.c` is vendored from [hackerschoice/zapper](https://github.com/hackerschoice/zapper)
(THC, https://www.thc.org).

## Authorized use only

Red-team engagements, CTFs, research, and your own machines.
