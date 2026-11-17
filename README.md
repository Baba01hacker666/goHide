# goHide

Run any command under a disguised process name. `sleep 3600` shows up as
`kworker/3:1` in `ps`, `top`, `htop`, and `/proc/*/comm` — right next to the
real kernel workers.

```
$ gohide -n "kworker/3:1" -- sleep 3600 &
$ ps -eo pid,comm,args | grep kworker
  6046 kworker/3:1     kworker/3:1          <- the gohide wrapper
  6056 kworker/3:1     kwork 30             <- the actual sleep 3600
```

## Build

```sh
make            # bin/gohide + bin/libgohide.so (needs go + gcc)
make test       # vet + unit tests
make smoke      # functional check
```

## Usage

```sh
gohide [flags] -- <command> [args...]

  -n, --as NAME    fake process name (required)
      --hide       hide the tree's pids from /proc listings it performs itself
      --wipe-args  child cmdline shows ONLY the fake name; the child sees
                   empty argv — only for programs that never read argv
                   (pass real args via env/stdin instead)

gohide preload install|remove|list   # manage /etc/ld.so.preload (root)
gohide version
```

Examples:

```sh
gohide -n "kworker/3:1" -- sleep 3600
gohide --as "systemd-resolved" -- python3 /opt/agent.py
gohide -n "[ksoftirqd/2]" --hide -- ./agent
gohide -n "dbus-daemon" --wipe-args -- /opt/srv   # args via env
```

## How it works

Two pieces:

1. **The wrapper** (`gohide`, Go + cgo) — sets its own `comm` via
   `prctl(PR_SET_NAME)` and blanks its whole argv string region on the stack,
   so the wrapper itself shows only the fake name in `ps aux`.

2. **The shim** (`libgohide.so`, C) — injected into the child via
   `LD_PRELOAD`:
   - `prctl(PR_SET_NAME)` fakes `comm` (what `ps`/`top`/`htop` show by
     default, max 15 chars).
   - Rewrites `argv[0]` in the stack memory of the child, faking the first
     token of `/proc/PID/cmdline` (`ps -f`, `ps aux`).
   - `--hide`: hooks `readdir`/`readdir64` so /proc directory listings
     performed by the tree skip its own pids (libprocesshider style).
   - `--wipe-args`: additionally blanks the child's whole argv region.

The fake name propagates to every dynamic child in the tree, so the whole
subtree wears the same disguise.

If no prebuilt `libgohide.so` is found next to the binary, gohide compiles
the embedded shim source to `~/.cache/gohide/` at first run.

## Hiding from *other* processes' ps (system-wide)

`--hide` only affects listings made by the hidden tree itself. To hide pids
from `ps`/`top` run by anyone on the box, install the shim system-wide:

```sh
sudo gohide preload install
# then, e.g. in /etc/environment:
GOHIDE_PIDS="1234,5678"     # pids to filter out of every ps/top
```

That works because every process (including `ps`) then loads the shim and
filters the listed pids out of `/proc` readdir results.

## Limitations

- Linux only (`prctl`, stack argv layout).
- Spoofing works on **dynamically linked** programs — most things
  (python, php, node, bash, coreutils). Static Go/C binaries ignore
  `LD_PRELOAD` and keep their real name; the wrapper itself is still
  disguised.
- The child's argv[0] fake is capped at its original length (`sleep` →
  `kwork`); the wrapper's own cmdline is fully wiped, no cap.
- A determined observer still wins: `ls /proc` lists the pid (unless
  hidden/shimmed), `/proc/PID/exe` reveals the binary, and root can always
  see everything. This is masquerading, not kernel-level concealment.
- `LD_PRELOAD` is ignored by setuid binaries.

## CI / Release

- `.github/workflows/ci.yml` — vet, build, unit tests, two smoke tests,
  gofmt check on every push/PR.
- `.github/workflows/release.yml` — on `v*` tags: cgo cross-builds for
  linux/amd64 + linux/arm64, packages tarballs + sha256, publishes a GitHub
  release with `gh release create --generate-notes`.

```sh
git tag v0.1.0 && git push origin v0.1.0
gh release view v0.1.0
```

## Authorized use only

Masquerading tools are for red-team engagements, CTFs, security research,
and hiding your own processes on machines you control. Don't be a clown
with it on systems you don't own.
