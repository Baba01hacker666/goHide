# Changelog

## Unreleased — -pt comm spoofing

- New `-pt` flag / `GOHIDE_PT=1`: injects `prctl(PR_SET_NAME)` into the target
  so the kernel comm is faked too. That covers plain `top`/`htop`, `ps -o comm`,
  and `/proc/PID/comm`, which argv-based hiding alone can't reach.
- Implemented in both the zapper core (`-pt` / `-p <name>`) and the pure-Go
  engine in `experimental/go-engine/`.

## Unreleased — zero-config CLI

- `gohide <command> [args...]` now just works: a random kworker name is chosen
  automatically. No `--` separator or `-n` required.
- Advanced options moved to env vars (`GOHIDE_NAME`, `GOHIDE_FOLLOW`,
  `GOHIDE_KEEP_ENV`, `GOHIDE_PID`).
- Pure-Go ptrace engine stashed in `experimental/go-engine/` for later.

## Unreleased — ptrace engine

Replaced the `LD_PRELOAD` shim with the ptrace engine vendored from
[hackerschoice/zapper](https://github.com/hackerschoice/zapper):

- Works on static binaries; no `LD_PRELOAD` or libc hooks.
- Wipes `/proc/PID/cmdline` and `/proc/PID/environ` right after `execve()`;
  the target still sees its real argv/env, so it keeps working.
- Go launcher is now cgo-free and `exec`s the engine, so no wrapper process
  lingers.
- New flags: `-f/--follow`, `-E/--no-env`, `--pid`. `--wipe-args` and `--hide`
  are accepted as deprecated no-ops.
- Dropped `libgohide.so`, system-wide `/etc/ld.so.preload` pid filtering, and
  the project's own `comm` spoofing (the engine fakes the full cmdline, not
  the kernel comm field).
- Release tarballs now ship `gohide-zapper` instead of `libgohide.so`.

## v0.1.0 — 2026-11-17

First release.

- `gohide` wrapper (Go + cgo): fakes its own `comm` via `prctl(PR_SET_NAME)`
  and blanks its own argv memory, so the wrapper itself shows only the fake
  name in `ps` / `top` / `htop`.
- `libgohide.so` shim (C, embedded in the binary, built on demand or shipped
  in release tarballs): injected into children via `LD_PRELOAD`; fakes the
  child's `comm` and rewrites `argv[0]` in stack memory to fake the first
  token of `/proc/PID/cmdline`.
- `--hide`: hooks `readdir`/`readdir64` so the tree's own pids are filtered
  from `/proc` listings it performs itself.
- `--wipe-args`: blank the child's full cmdline (child sees empty argv).
- `gohide preload install|remove|list`: manage `/etc/ld.so.preload` for
  system-wide pid hiding (libprocesshider style).
- CI: vet, unit tests, two functional smoke tests, gofmt gate.
- Release: cgo cross-builds for linux/amd64 and linux/arm64, tarballs +
  sha256 checksums, published with `gh release create`.
