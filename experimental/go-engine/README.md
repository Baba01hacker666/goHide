# go-engine (WIP)

Pure-Go version of the ptrace engine, stashed here to finish later. The released
`gohide` currently wraps the C engine from hackerschoice/zapper instead.

Idea: stop the tracee after `execve()`, move argv/env to a fresh part of the
stack, wipe the old copy, fix the ELF aux table, resume. Same technique as
zapper, but implemented with raw `PTRACE_*` syscalls so there is no vendored C
and no compiler needed at runtime.

Status:
- amd64 stack rebuild and register handling written in `engine.go`.
- `followTrace()` still needs testing against real fork/exec trees.
- Needs arm64 support and a proper test suite before replacing the wrapper.

Build: `cd experimental/go-engine && go build ./...`
