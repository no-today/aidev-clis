# AGENTS.md

You're iterating on **aidev-clis**: a suite of atomic-capability CLIs that let an
AI agent read/observe/operate on running systems without ever seeing a credential.
This file is the entry point for that work. Humans start at
[README.md](README.md); agents *using* a CLI at runtime read the matching
`skills/aidev-<cli>/SKILL.md`; this file is for changing the repo itself.

## Iron laws (do not violate)

- **AI-first output.** Every command defaults to a JSON envelope. `--output raw`
  is opt-in human form, never the default. See [docs/OUTPUT-CONTRACT.md](docs/OUTPUT-CONTRACT.md).
- **Adapter isolation.** No cross-CLI Go imports, with the single sanctioned
  exception that `aidev` reads apicli's config package (read-only) for discovery.
  The `make check` isolation guard enforces this. See
  [docs/ADAPTER-ISOLATION.md](docs/ADAPTER-ISOLATION.md) and
  [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).
- **Concurrent-session isolation.** Multiple sessions share this working tree and
  branch. Implement in a sibling git worktree (`aidev-clis-<feature>` convention),
  never edit the live tree directly, or commits interleave.
- **`-h` is the truth source for flags.** Docs describe the durable model and
  gotchas; they must not re-enumerate flags. If you add or change a flag, update
  `-h`/code — not a flag table in prose.
- **Specs and plans live in** `docs/superpowers/specs/` and
  `docs/superpowers/plans/`. Brainstorm → spec → plan → implement.

## Build / test / run

```sh
go build ./...        # build all CLIs
go test ./...         # full test suite
make check            # gofmt + vet + build + isolation guards + tests (pre-commit subset)
make hooks            # install the pre-commit hook once, after cloning
make install          # build + install binaries, skills, completions
```

CI (`.github/workflows/ci.yml`) runs `go test` on Linux/macOS/**Windows**. Before
adding tests or platform-specific code, read the [cross-platform
contract](docs/CROSS-PLATFORM.md) — POSIX perms, path separators in YAML/shell,
`.exe` on exec, and the bash/PowerShell install-script parity. You can't run the
Windows leg on a Mac/Linux box; `make check` crossbuilds all three GOOS.

## Where things live

- `cmd/<cli>/` — CLI entry, flag wiring, command registration.
- `internal/<cli>/` — per-CLI logic.
- `internal/core/` — shared: `envelope`, `audit`, `config`, `cred`, `allow`,
  `diag`, `errs`, `exec`.
- `adapters/db/<driver>/` and `adapters/log/<adapter>/` — self-contained backends.
- `docs/` — stable contracts + thin per-CLI pages. `skills/` — agent runtime docs.

## How to add X

- **A db driver:** create `adapters/db/<name>/`, register it in
  `cmd/dbcli/register.go`. Keep credential injection, arg validation, secondary
  allowlist, and error codes inside the adapter.
- **A log adapter:** create `adapters/log/<name>/`, register it in the logcli
  command wiring (mirror an existing adapter).
- **An apicli app:** configured, not coded — a target in `apicli.yaml` plus its
  auth flow; see [docs/cli-apicli.md](docs/cli-apicli.md).

Deleting a capability should be cheap: remove the adapter directory and its one
registration line.
