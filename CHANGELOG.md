# Changelog

## v0.2.0 — 2026-11-19

- `gohide <command> [args...]` now just works: a random kworker name is chosen
  automatically. No `--` separator or `-n` required.
- Comm spoofing via injected `prctl(PR_SET_NAME)` is on by default, covering
  plain `top`/`htop`, `ps -o comm`, and `/proc/PID/comm`. `-no-pt` / `GOHIDE_PT=0`
  disables it; `-pt` is still accepted as a no-op. Implemented in both the
  zapper core (`-pt` / `-p <name>`) and the pure-Go engine in
  `experimental/go-engine/`.
- Advanced options live in env vars (`GOHIDE_NAME`, `GOHIDE_PT`,
  `GOHIDE_FOLLOW`, `GOHIDE_KEEP_ENV`, `GOHIDE_PID`).
- Replaced the `LD_PRELOAD` shim with the ptrace engine; static binaries work.
- Pure-Go ptrace engine stashed in `experimental/go-engine/` for later.

The engine is vendored from
[hackerschoice/zapper](https://github.com/hackerschoice/zapper):

- Wipes `/proc/PID/cmdline` and `/proc/PID/environ` right after `execve()`;
  the target still sees its real argv/env, so it keeps working.
- Go launcher is cgo-free and `exec`s the engine, so no wrapper lingers.
- Release tarballs ship `gohide-zapper` instead of `libgohide.so`.

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
