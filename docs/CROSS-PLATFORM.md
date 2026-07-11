# Cross-Platform Contract

The CLIs ship on **Linux, macOS, and Windows**. `.github/workflows/ci.yml` runs
`go test` on all three, plus a native-Windows install/runtime smoke — so
cross-platform support is enforced by CI, not aspirational. The binaries are
pure Go and cross-compile cleanly (`make check` crossbuilds every GOOS). The
rules below keep the binaries *and the tests* green everywhere. Most were
written after a real CI failure.

## What CI enforces

- **`test`** — `go build ./...` + `go test ./...` on `ubuntu-latest`,
  `macos-latest`, `windows-latest` (matrix, `fail-fast: false`).
- **`lint`** (Linux) — gofmt, `go vet`, the adapter isolation guard, and a
  crossbuild for windows/linux/darwin. Mirrors `make check` minus tests.
- **`windows-smoke`** (Windows) — PSScriptAnalyzer over the `.ps1` scripts, then
  `scripts/ci/windows-smoke.ps1`: install via `install.ps1`, exercise the CLIs
  on real Windows, then assert `uninstall.ps1` reverts PATH / `$PROFILE` /
  skills / binaries.

## Writing portable tests

The Windows leg is the one you cannot run on a Mac/Linux dev box, and it is
where portability bugs hide. Each rule below corresponds to a failure the
matrix actually caught.

**1. Do not assert POSIX permission bits unconditionally.** Windows is
ACL-governed; Go reports `0o666`/`0o777` regardless of the mode you passed to
`WriteFile`/`MkdirAll`. Guard the assertion:

```go
if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
    t.Errorf("mode = %o, want 600", info.Mode().Perm())
}
```

(The product already splits this concern: `internal/core/cred/perm_unix.go` vs
`perm_windows.go`, where `tooOpen` is a no-op on Windows.)

**2. Run a filesystem path through `filepath.ToSlash` before embedding it in
YAML or a shell command.** A `filepath.Join` path on Windows contains
backslashes. Inside a double-quoted YAML scalar, `\U`/`\A`/… parse as escape
sequences (`yaml: did not find expected hexadecimal number`); inside a `bash`
command, backslashes are consumed as escapes and a redirect target is mangled.
Forward slashes work on Windows for both.

```go
cfg := "... - [bash, -c, \"echo x > " + filepath.ToSlash(marker) + "\"]"
// read it back with the original (backslash) path — same file
```

**3. Append `.exe` when a test builds and then execs a binary.** `go build -o
<name>` writes exactly `<name>`; Windows cannot start an extensionless binary
path, so exec returns empty output.

```go
name := "apicli"
if runtime.GOOS == "windows" {
    name += ".exe"
}
bin := filepath.Join(t.TempDir(), name)
```

**4. Guard genuinely unix-only tests.** `bash` exists on `windows-latest` (Git
Bash), so a simple `bash -c` step is fine — but a test that writes and execs a
`#!/bin/sh` script, or asserts unix-only behavior, must guard with
`if runtime.GOOS == "windows" { t.Skip(...) }` or live in a `//go:build unix`
file. Precedents: `internal/core/cred/perm_unix_test.go`,
`cmd/tcli/integration_test.go`.

**5. Resolve home/config via the platform, not `$HOME`.** Go: `os.UserHomeDir()`
(honor `AIDEV_CLIS_HOME` first, per `internal/core/config`). PowerShell:
`%USERPROFILE%` — the Node-based agents use `os.homedir()` == `%USERPROFILE%`,
which can differ from PowerShell's `$HOME` (`HOMEDRIVE+HOMEPATH`, a network share
on some domain machines).

## Install scripts stay in parity

Two installers, kept behavior-equivalent:

- `scripts/install.sh` / `uninstall.sh` — bash, for Linux/macOS (and Windows via
  WSL).
- `scripts/install.ps1` / `uninstall.ps1` — PowerShell, for native Windows
  (user scope, no admin).

Windows-installer conventions (each learned the hard way):

- **Build straight into the install dir.** Repo `bin/` is gitignored, and
  `go build -o` will not create a missing parent directory.
- **User PATH** via `[Environment]::SetEnvironmentVariable(..., 'User')` (it
  broadcasts `WM_SETTINGCHANGE`), never `setx` (truncates PATH at 1024 chars).
  Force the split into an array with `@(...)` so a single-entry User PATH (the
  Windows default) *appends* rather than string-concatenating.
- **Skills** into `%USERPROFILE%\.claude\skills` (+ `.codex\skills`);
  **completion** dot-sourced from `$PROFILE.CurrentUserAllHosts` via an
  idempotent marker block.
- Keep the scripts **pure ASCII** (no BOM ambiguity under Windows PowerShell
  5.1) and **PSScriptAnalyzer-clean** (`scripts/PSScriptAnalyzerSettings.psd1`
  documents the one intentional exclusion).

## Verifying locally

You cannot run Windows `go test` on a Mac/Linux box. Options, cheapest first:

- `make check` — crossbuilds all three GOOS (compile-only; catches build breaks,
  not runtime).
- Lint the PowerShell:
  `pwsh -c "Invoke-ScriptAnalyzer -Path scripts\install.ps1 -Settings scripts\PSScriptAnalyzerSettings.psd1"`.
- Run `scripts/ci/windows-smoke.ps1` in a Windows VM, or push and let the
  `windows-smoke` CI job run it.
