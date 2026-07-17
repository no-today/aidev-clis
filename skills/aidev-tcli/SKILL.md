---
name: aidev-tcli
description: Use to run a YAML case that orchestrates apicli/dbcli/logcli (as subprocesses) and emits a conclusion-first PASS/FAIL verdict with a CI exit code. Reach for it when you want to ENCODE an assertion and re-run it — either a one-shot post-deploy gate (API→DB(→log) causal chain) or a durable regression suite guarding a stable cross-system invariant (row-level equivalence, conservation, cross-source consistency) that no single service's unit tests can express. Trigger is intent (a kept, asserted, re-runnable check), not step count — a single-query invariant counts. For a one-off answer you'll just read, use apicli/dbcli/logcli directly. Covers run/validate/explain, the declare-before-use apps/dbs/logs schema, the uniform expect: list, capture/templating, actors, wait/trace/safety.
---

# tcli

`tcli` runs a YAML *case* that drives the other CLIs — `apicli`, `dbcli`, `logcli`
— purely as **subprocesses over their JSON `{data}`/`{error}` contract** (it never
imports their Go packages), asserts an API → DB (→ log) chain **or** a cross-system
DB invariant, and emits a **conclusion-first** verdict with a CI-friendly exit code.

A case has one of **two lifecycles**:
- **one-shot post-deploy gate** — disposable, written to confirm a specific change
  reached production;
- **durable regression suite** — kept and re-run, guarding a **stable cross-system
  invariant** (row-level equivalence, financial conservation, cross-source
  consistency) that **no single service's unit tests can express**.

Regression a single service *can* unit-test belongs in that service's own CI, not here.

## When to reach for tcli (the one rule)

The trigger is **intent, not step count**: reach for tcli when you want to **encode
an assertion and get a re-runnable PASS/FAIL verdict** — whether that's a ≥2-step
causal chain (API write → DB read → log) or a **single query asserting an invariant
you'll keep**. For a **one-off answer you'll just read** (no assertion to keep, no
verdict needed), use `apicli` / `dbcli` / `logcli` **directly**.

| the task | reach for |
|---|---|
| "did the create-order call actually land in the DB?" (API→DB gate) | **tcli** |
| "guard that the report table still equals its upstream reconciliation feed" (kept invariant, one SQL) | **tcli** (regression) |
| "is the health endpoint up, just checking?" | `apicli` directly |
| "run this SQL / read this key, show me the rows" | `dbcli` directly |
| "tail/grep these logs" | `logcli` directly |

Both lifecycles are legitimate. What's NOT tcli: regression a single service can
cover in its own unit tests (see "Don't use tcli for" below).

```
tcli run      <case-or-dir> [--tag t ...]   # execute; exit 0 PASS / 1 FAIL|ERROR / 2 blocked|invalid
tcli validate <case-or-dir> [--tag t ...]   # parse + static checks only, no calls (safe)
tcli explain  <case-or-dir> [--tag t ...]   # resolved target -> app/actor/target mapping, no execution
tcli completion <shell>                     # shell completion script
```

- `<case-or-dir>` — a single `.yaml`/`.yml` case, or a directory (each file is a
  case; a directory yields a suite result surfacing the first failing case).
- `--tag <t>` — only run cases whose `tags` include **every** `--tag` (repeatable, AND).
- Global: `--config-dir <path>` (sets `AIDEV_CLIS_HOME`, passed to every
  subprocess), `--pretty`, `-v`/`-vv` (adds a `diagnostics` array).

`tcli` has **no config of its own**. Capabilities map to apps/targets declared in
`apicli.yaml` / `dbcli.yaml` / `logcli.yaml`. Child binaries are found next to the
`tcli` executable first, then on `PATH`.

## Validate/explain before you run

`validate` (parse + static checks, no calls) and `explain` (show the resolved
app/actor/target mapping, no execution) are **safe** — run them first on a new or
edited case. Only `run` makes real calls. When authoring a case, iterate with
`validate` until it's clean, then `explain` to confirm targets resolve, then `run`.

## Case file — declare capabilities up front, then run ordered phases

```yaml
name: order service post-deploy gate
tags: [smoke, orders]
apps:                              # api alias -> identity (app defaults to the alias)
  buyer: { app: orders,  actor: qa }
  admin: { app: billing, actor: qa_admin }
dbs:   { main: orders_mysql, cache: orders_redis }   # alias -> dbcli target
logs:  { svc: orders_logs }                          # alias -> logcli target
vars:   { customer: "C-1001" }     # plus built-ins {{run_id}} {{uuid}} {{now}}
safety: { allow_db_write: false }  # write SQL refused unless true (+ step.write)
setup:      [ ... ]                # runs first; failure stops the case
steps:      [ ... ]                # main flow; stops at first failure
assertions: [ ... ]                # standalone checks over captured vars
cleanup:    [ ... ]                # always runs, best-effort, never changes verdict
```

**Declare-before-use.** Every step picks its capability with `target: <alias>` into
`apps`/`dbs`/`logs`. When a section has exactly one entry, its steps may omit
`target`. An undeclared target (or an omitted target with multiple entries) is a
`validate` error — so the top of the case is the full list of what it touches.

**Phases.** `setup` failure skips `steps`+`assertions`; a `steps` failure skips
`assertions`; `cleanup` **always** runs (a cleanup step with an unset template var
is skipped, not errored). `vars` are shared across phases; built-ins are seeded
once per run.

## Actions — each is exactly one of api / db / log / standalone assertion

```yaml
# api -> apicli call (the request: block is raw HTTP parsed into method/path/headers/body)
- name: create order
  api:
    target: buyer
    request: |
      POST /api/v1/orders
      Content-Type: application/json

      {"customer":"{{customer}}"}
    expect:  [ "status_code == 200", "body.code == 0", "body.data.id exists" ]
    capture: { id: body.data.id }
    save_body: /tmp/out.xlsx        # optional -> apicli --output-file (no body to assert then)
    wait: { timeout: 30s, interval: 2s }

# db -> dbcli <driver> --target <target> [--allow-write] "<statement>"  (driver auto-discovered)
- name: verify row
  db:
    target: main
    sql: "SELECT status FROM orders WHERE id = '{{id}}'"
    write: false                    # true requires safety.allow_db_write (else SAFETY_BLOCKED)
    expect:  [ "count == 1", "rows.0.0 == confirmed" ]
    capture: { db_status: rows.0.0 }

# log -> sls trace/search, or non-sls logs + client-side grep  (adapter auto-discovered)
- name: correlate in logs
  log: { target: svc, trace: "{{id}}", from: 15m, expect: [ "count >= 1" ] }

# standalone assertion (no backend): expressions over captured vars
- name: api matches db
  expect: [ "{{db_status}} == confirmed" ]
```

A case may span drivers — name a different `target` per db step; `sql` is that
target's driver statement (SQL, a redis command, or a mongosh statement).

## expect — one uniform expression list

Every step (and standalone assertion) carries `expect:`, a list of `<lhs> <op>
<rhs>` expressions — no assertion "keys" to memorize. Operators: `==` `!=`
`contains` `not contains` `matches` (regex) `is` (string|number|bool|array|object|null,
result paths only) `exists` (no rhs) `not exists` (no rhs) `>=` `>` `<=` `<`, and
`count <op> n` (collection length).

- `path` is **result-relative** (NO `data.` prefix): api → `status_code`,
  `body.code`, `headers.X-Trace-Id`; db (SQL/redis) → `rows.0.0`, `columns.0`; db
  (mongo) / log → `0.field` (the document/record array root).
- `count` is the collection length: `body` (api), `rows` (SQL/redis), the root
  array (mongo find / log).
- Desensitization pattern: `body not contains <plaintext>` matches against the
  whole serialized body, so it survives field moves.
- Assert ID type contracts (`… is string`) on `api` steps, not `db` steps —
  dbcli emits int64 beyond ±2^53 as a string by design (JS-precision guard).
- `{{vars}}` are rendered everywhere before evaluation; a malformed expression
  fails `validate` (`EXPECT_INVALID`).

## capture, templating, identity, safety

- **capture:** `capture: { var: <path> }` stores a result-relative value into
  `vars`. `{{var}}` substitutes from vars + captures + built-ins `{{run_id}}`
  (unique — mint collision-free ids), `{{uuid}}`, `{{now}}`; an undefined var is
  `TEMPLATE_VAR_MISSING` (a `log` needle or `cleanup` var that's unset SKIPs).
- **identity:** an `apps` entry's `actor` is a **name** in `actors.yaml` or an
  **inline** `{ vars: { username: u, password: p } }`. tcli never logs in itself —
  apicli captures/relogs the session; an auth failure surfaces an
  `apicli login <app> --actor <name>` hint.
- **wait/retry:** `wait: { timeout, interval }` retries a step until its `expect`
  passes or times out (default 30s/2s when present). Retries fire only on
  assertion failures and retryable remote/timeout errors; config/auth/safety
  failures stop immediately.
- **trace = grep:** a `log` step's needle (`trace:` exact id or `query:` expr) is
  any captured value; on `sls` it uses the indexed `logcli sls trace`, other
  adapters grep a bounded `logs` slice. An empty/undefined needle just SKIPs.
- **safety:** a `db` step with `write: true` needs `safety.allow_db_write` or tcli
  returns `SAFETY_BLOCKED` before calling dbcli (dbcli's own guard re-enforces it).
  tcli never reclassifies SQL.
- **live-data cases assert invariants, not snapshots:** against prod/UAT tables
  that keep moving, pinning absolute values (row counts, money totals) makes the
  same case FAIL→PASS on identical reruns. Assert relations that survive drift —
  set-equivalence between two queries, `count >= n`, field presence — and pin
  absolutes only on data the case itself created (via `{{run_id}}`).
- **PII:** seed and expect values are always fake-but-format-valid or surrogate
  keys — never real PII copied from a live system (full rules: the
  **aidev-dbcli** skill).

## Output contract (parse this)

`run` emits `{"data": <CaseResult>}` **conclusion-first**: `verdict`
(`PASS|FAIL|ERROR|SAFETY_BLOCKED`), `summary`, then a `failure` object with
`phase`/`step`/`code`/`message`, redacted+truncated `evidence`, and `next[]`
suggested commands — followed by `counts` and per-phase step detail. A ran-but-
failed gate is still a `{data}` envelope (non-zero exit), mirroring apicli. **Exit
codes: 0 PASS / 1 FAIL|ERROR / 2 SAFETY_BLOCKED|invalid.** `-v`/`-vv` add
`diagnostics`; `--pretty` indents.

## Don't use tcli for

- **Regression a single service can unit-test** — put lasting per-service tests in
  that service's own CI. tcli's durable niche is the **cross-system** invariant no
  single service's unit tests can express (row-level equivalence, conservation,
  cross-source consistency).
- **A one-off answer you'll just read** — to hit one endpoint / run one query /
  read logs and just look, use `apicli` / `dbcli` / `logcli` directly. tcli earns
  its keep when you want the assertion + verdict kept and re-runnable.
- **Anything the child CLIs aren't configured for** — tcli adds no new
  credentials or targets; it composes what apicli/dbcli/logcli already expose.

Two example cases are bundled alongside this skill: `tcli-case-minimal.yaml` (a
runnable starting point) and `tcli-case.yaml` (the fully-commented schema —
setup/cleanup, logs, trace, assertions, script asserts, safety). They are
reference templates — copy the minimal one and adapt it into the case you need.
See also `docs/cli-tcli.md`.
