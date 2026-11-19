# Adapter Isolation Contract

Each adapter is fully self-contained — including its credential injection and
its secondary allowlist — so retiring it removes its security rules along with
it. This is a hard architectural constraint, not a style preference.

## The five rules

**1. The interface is the only contact surface.**
Core and CLI-skeleton code names the interface, never a concrete adapter
package. An `isolation-guard` script (run as part of `make check`) greps for
any concrete adapter import path in Go files outside its own package directory
and its CLI's `register.go`, and fails the build if found.

**2. Registration is one line.**
`cmd/<tool>/register.go` is a flat list of adapter registrations. Adding an
adapter means adding one line; retiring an adapter means deleting one line and
the package directory. Core code and other adapters never change.

**3. No per-adapter hooks in the core.**
Special authentication (request signing, kubeconfig injection), the secondary
allowlist, argument parsing — all of this lives inside the adapter package.
The core has no `if adapter == "sls"` blocks. None.

**4. Self-containment.**
Each adapter owns, entirely within its own package:
- argument handling and validation
- credential injection
- its secondary verb/statement-type allowlist
- config parsing for its own fields
- its error codes
- its third-party dependencies

**5. Test isolation.**
CLI-skeleton tests use a stub adapter. Each adapter ships its own tests.
Removing an adapter cannot break a core or skeleton test — if it does, rule 3
or rule 4 was violated.

## Retirement procedure

To retire an adapter named `<name>`, remove its package under `adapters/db/` or
`adapters/log/` and delete its registration line and import from the owning
CLI's `register.go`. Run `make check` to confirm isolation.

## CLI isolation

Each CLI (`httpcli`, `dbcli`, `jcli`, `logcli`, `tcli`) is standalone with **zero
coupling to its siblings**. The allowed import surface for code under
`cmd/<cli>/` or `internal/<cli>/` is:

- `internal/<cli>/**` — its own packages,
- `internal/core/**` — the shared leaf layer (envelope, errs, config, audit,
  cred, …), and
- its own adapters, via `cmd/<cli>/register.go` only.

A CLI importing another CLI's `internal/<otherCli>/**` is a contract violation.
Shared logic belongs in `internal/core/**`, never reached for sideways from one
CLI into another. This keeps each tool independently understandable, testable,
and removable.

The guard covers the five standalone CLIs above. `aidev` discovery reads
httpcli's config package as its one permitted cross-CLI import.

## The `make check` isolation guard

`scripts/check-isolation.sh` (invoked by `make check`) enforces two invariants
and fails the build with no override flag if either is violated.

**Adapter isolation.**

1. For each adapter, identify its path `adapters/<category>/<name>` and owning CLI.
2. Search the repository's Go files for that path.
3. Fail if any match appears outside:
   - files under `adapters/<category>/<name>/` itself, and
   - `cmd/<tool>/register.go`.

**CLI isolation.**

1. For each CLI and each sibling CLI, build the sibling's internal package root
   `<module>/internal/<otherCli>`.
2. Grep this CLI's own trees (`cmd/<cli>/`, `internal/<cli>/`) for that path.
3. Fail if any match appears — a CLI must never import a sibling's internals.
