# Changelog

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
