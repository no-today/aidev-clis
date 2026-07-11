# Architecture

## First principles

An AI agent doing real engineering work is only as good as its **feedback loop** —
the cycle of *make a change → find out whether it worked*. The tighter that loop,
the faster the agent converges on a correct answer.

The tightest possible loop is local: **edit → compile → unit test**. It runs in
seconds, needs nothing but the source tree, and the result is unambiguous. When
this loop is available, nothing here beats it — prefer it, always.

But much real work happens where that loop does not exist or is too slow: legacy
services with no unit tests, behaviour that only appears against a real database,
bugs that only surface in production logs. There the single source of truth is
the **running system itself**. To get an answer the agent has to observe that
system's state, and sometimes operate its levers.

These CLIs exist to give an agent that access. Each one is an **atomic
capability** over a running system, with structured (AI-first) output, injected
credentials, and an audit trail — built to give an autonomous agent a narrower,
more legible interface than credentials plus a general-purpose shell.

## Runtime call path

The caller — an AI agent, CI job, or operator — invokes one standalone CLI. The
CLI resolves its named target or app from the local config directory, loads the
required credential or session, dispatches the request to its database, log,
HTTP, or Jenkins backend, and emits both the uniform envelope and an audit
record. Config, credential, session, envelope, and audit primitives form
a shared local control layer; backend behavior remains owned by each CLI and
its adapters.

CLIs do not call each other in-process. `tcli` composes `apicli`, `dbcli`, and
`logcli` by launching their binaries and consuming their public JSON contract.
The only sanctioned cross-CLI Go import is `aidev` reading apicli's config
package to build a read-only capability inventory; it never dispatches an
apicli operation.

## The capability hierarchy

Ordered by how fundamental and how universal each capability is.

### Observe — the core

Every running system has *state* and a *trail*. Reading them is the most basic,
most universal thing an agent needs, and it is read-mostly and low-risk. This is
where most questions actually get answered.

- **dbcli** — the system's state. Query (and, with explicit `--allow-write`
  intent, mutate) any configured database through a typed driver that returns
  structured rows. The driver produces JSON natively — no `mysql` binary, no text
  scraping. Drivers: mysql, postgres, kingbase, redis, sqlite, mongo (+ dataease
  as a last-resort read-only HTTP bypass). Read/write guard and statement
  classification keep accidental writes out.
- **logcli** — the system's trail. List, read, follow (`-f`), search, and trace a
  request across services, over kubectl / docker / SSH hosts / local files /
  Aliyun SLS.

Every system has a database and logs. dbcli and logcli are the load-bearing core.

### Act — secondary

Less universal, because each assumes a particular *shape* of system — an HTTP
surface, a Jenkins instance. They change the world, so they carry more risk and
need more care.

- **apicli** — call the system's HTTP surface with a captured session
  (cookie/header replay, auto-relogin, per-app response predicates). Private-IP
  and plain-HTTP are intentionally allowed (dev VMs, bastions, pod IPs).
- **jcli** — trigger a Jenkins build or a deploy and watch it roll out
  (`build` / `status` / `log -f` / `cancel` / `deploy`).

### Verify — a fallback, not a goal

- **tcli** — composes the CLIs above into a post-deploy verification gate: drive
  an API, then assert what landed in the database and the logs (API→DB(→log)
  chains across setup / steps / assertions / cleanup).

tcli exists *only* for systems where the ideal loop — unit tests with immediate
compile feedback — is not available. Verifying through tcli means taking the long
way around: merge, build, release, *then* observe. That cycle is slow and it
churns commit history. So tcli is a last resort, reached for when there is no
faster way to learn the truth. **Whenever a unit test can answer the same
question, write the unit test instead.** Most tcli cases are therefore disposable
one-shot gates — confirm a change reached production, then discard.

The exception worth keeping is a **cross-system invariant** that no single
service's unit tests can express: row-level equivalence between an offline
reporting table and an external reconciliation file, financial conservation across
split/merged orders, consistency between two sources of the same fact. Those form a
**durable regression suite** — tagged `regression`, re-run as a bulwark whenever
related code changes. What still belongs in a service's own CI is any regression
that service can unit-test alone.

Either way, tcli composes the other CLIs by contract (shelling out), never by
importing their Go.

### Orient — discovery

Before observing or acting, an agent needs to know what this workspace even
exposes.

- **aidev** — read-only cross-CLI discovery. Resolves the active workspace scene
  (nearest `.aidev.yaml` or `AIDEV_SCENE`) and joins every CLI's config into a
  single `tools` / `targets` / `apps` inventory (`apps` is a sorted list of
  in-scope apicli app names). Never dispatches. The one mutating exception is
  `aidev config backup` / `aidev config restore` (backed by
  `internal/aidev/configarchive`), which bundles `~/.aidev-clis` — the top-level
  `*.yaml` files plus `credentials/` — into a `.tar.gz`; restore takes a
  safety backup first (unless `--no-backup`) and rejects path-traversal/symlink
  archives.

## What makes them one family

- **AI-first by default.** Every command emits the minimal `{data}` / `{error}`
  JSON envelope. `--output raw` is the opt-in human form — never the default.
- **One atomic capability each.** No god-CLI. Each binary does one thing; you
  compose them, the tool doesn't pre-compose for you.
- **Typed output, no scraping.** Adapters/drivers return structured data, not
  text that someone has to parse back out.
- **Bounded and legible for an agent to drive.** Credentials are injected and
  never logged (`0600`-enforced); read-only defaults with explicit write
  switches; every invocation appends an audit line; secondary
  allowlists gate anything that shells out. This reduces accidents and keeps
  actions auditable — it is not a sandbox against a same-user process
  (see docs/SECURITY-MODEL.md).
- **Compose by contract, not by import.** Cross-CLI composition (tcli, aidev)
  goes through the CLI surface and the envelope, with one sanctioned exception:
  `aidev` discovery reads apicli's config package directly (read-only, for the
  capability inventory) — it dispatches nothing. `tcli` takes no such liberty:
  the `make check` isolation guard (`scripts/check-isolation.sh`) lists it among
  the standalone CLIs and fails the build on any sibling `internal/` import.
  (`aidev` is deliberately outside that CLI set, which is what permits its one
  read-only import.)

## Repo layout

```
aidev-clis/
  go.mod
  cmd/{logcli,dbcli,jcli,apicli,tcli,aidev}/   main.go  (+ register.go for adapter CLIs logcli, dbcli)
  internal/
    core/{config,cred,envelope,errs,audit,exec,clihelp,allow,diag}/
    logcli/ dbcli/ jcli/ apicli/ tcli/ aidev/   # per-CLI logic (aidev = cross-CLI discovery)
  adapters/
    log/{aliyun_sls,kubectl,docker,ssh_file,ssh_docker,local_file}/
    db/{mysql,pg,kingbase,redis,sqlite,mongo,dataease}/
  examples/                                 # neutral sample configs (per-CLI yaml, .aidev.yaml, tcli-case[-minimal].yaml); install.sh/.ps1 copy each into its skill dir
  docs/
  scripts/{check-isolation.sh,install.sh,install.ps1,uninstall.sh,uninstall.ps1,get.sh,get.ps1,PSScriptAnalyzerSettings.psd1,githooks/,ci/}
  Makefile                                  # check = crossbuild+vet+gofmt+tests+guards
```

Single `go.mod`. Each CLI in `cmd/` produces an independent binary. Adapters live
under `adapters/`, grouped by domain — only `logcli` (log) and `dbcli` (db) use
the registered-adapter pattern; `jcli`/`apicli`/`tcli` carry their logic directly
in `internal/<cli>/`, and `aidev` is a cross-cutting reader of the other CLIs'
configs (no adapters).

## `internal/core` packages

Each package has exactly one responsibility, and none of them know any adapter —
a core package never imports an adapter.

| Package | Responsibility |
|---|---|
| `config` | Load `~/.aidev-clis/<tool>.yaml` (+ optional `.aidev.yaml`); resolve the chosen target/instance; `--config-dir` / `AIDEV_CLIS_HOME` override. |
| `cred` | Read `~/.aidev-clis/credentials/<name>` (0600 enforced); hand the bytes to the adapter; never log them. |
| `envelope` | The minimal `{data}` / `{error}` JSON writer + NDJSON streaming + `--pretty`. One writer shared across all CLIs. Default is the AI-first JSON envelope; `--output raw` is the opt-in un-enveloped human form (incl. plain `ERROR <code>: <message>` lines). |
| `errs` | `Error{Code, Message, Exit}`; exit classes: `0` ok / `1` general / `2` config / `3` auth / `4` timeout / `5` remote. |
| `audit` | Write one JSON line per invocation to `~/.aidev-clis/audit/<YYYYMMDD>.jsonl` (one file per UTC day; 30-day auto-prune). Each line carries `tool`, `backend`, `target`, `command` (full argv), optional `request` (resolved backend params — apicli/jcli only), `result` summary, error `code`, `outcome` (`ok` or `error`), `duration_ms` (terminal lines only), and optional `session` for correlating lines from one agent session (read from `AIDEV_SESSION`, falling back to Claude Code's `CLAUDE_CODE_SESSION_ID`; omitted when neither is set). Audit payloads are not redacted and may contain sensitive business data; response bodies are discarded, with only result summaries retained. Credential bytes stay absent because adapters inject them separately. Side-effecting operations (dbcli writes, jcli build/deploy/cancel, apicli non-GET calls) are two-phase: a `started` line is written before dispatch and a terminal line sharing the same `id` is written after — so a hard kill still leaves a record that the operation was issued. All writes are file-locked to prevent torn lines under concurrent access. |
| `exec` | Run a native subprocess with injected env/auth, captured + streamed output, context cancellation, stderr redaction. Used by pass-through adapters (kubectl/docker/ssh). |
| `clihelp` | Opt-in cobra helpers: `--output`, `--timeout`, `--config-dir`, `-v/-vv`, signal-aware context. **Not a forced parent** — each CLI opts in. |
| `allow` | Secondary allowlists (e.g. binaries permitted in a `jcli` deploy's `steps`) — defense-in-depth on top of the backend's own authorization. |
| `diag` | Opt-in `-v`/`-vv` diagnostics collector, drained into the envelope's `diagnostics` field (absent without `-v`). |

## Surface model

Each CLI is a thin skeleton — a root cobra command, a backend dispatcher, and a
**typed adapter interface** tailored to that CLI's domain. The skeleton is thin;
the substance lives in the adapters.

```
<cli> <backend> [--target <name>] <native input...>
```

The backend is the first positional argument and maps directly to a cobra
subcommand. `--target` selects which configured instance to use. Everything after it
is native input passed through to the adapter unchanged:

```sh
dbcli  mysql   --target uat  "SELECT id FROM orders WHERE status='NEW' LIMIT 5"
logcli kubectl --target uat  logs -f -l app=pbs-ca
logcli docker  --target box  logs -f --tail 200 <container>
```

Backends with no native CLI (e.g. `sls`) use a small custom verb set
(`search` / `trace` / `tail`) instead of pass-through.

There is **no shared `--target` superclass** across CLIs. `apicli` addresses apps
(each with its own base URL and session under `~/.aidev-clis/sessions/`), not
named targets — forcing a common parent would needlessly constrain. The core
supplies primitives; each CLI shapes its own command tree.

Backend registration is intentionally minimal: `cmd/<cli>/register.go` is a list
of one-line adapter registrations. Retiring a backend = delete the adapter package
directory and its import line. Nothing else changes.
