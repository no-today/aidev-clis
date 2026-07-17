# tcli

tcli runs a YAML *case* that orchestrates the other CLIs — `apicli`, `dbcli`,
`logcli` — purely as subprocesses over their JSON `{data}`/`{error}` contract (it
never imports their Go packages), asserts an API → DB (→ log) chain or a
cross-system DB invariant, and emits a **conclusion-first** verdict with a
CI-friendly exit code.

A case has one of two lifecycles: a **one-shot post-deploy gate** (disposable —
confirm a specific change reached production) or a **durable regression suite**
(kept and re-run — guard a stable cross-system invariant that no single service's
unit tests can express). Regression a single service *can* unit-test belongs in
that service's own CI — see `docs/ARCHITECTURE.md`.

**When to reach for tcli:** the trigger is *intent, not step count* — when you want
to encode an assertion and get a re-runnable PASS/FAIL verdict, whether that's a
≥2-step causal chain (API write → DB read → log) or a single query asserting an
invariant you'll keep. For a one-off answer you'll just read, use
`apicli`/`dbcli`/`logcli` directly.

## Synopsis

```
tcli run      <case-or-dir> [--tag <t> ...]      # execute; exit 0 PASS / 1 FAIL|ERROR / 2 blocked|invalid
tcli validate <case-or-dir> [--tag <t> ...]      # parse + static checks only, no calls (safe)
tcli explain  <case-or-dir> [--tag <t> ...]      # resolved target -> app/actor/target mapping, no execution
```

- `<case-or-dir>` — a single `.yaml` case, or a directory (each `*.yaml`/`*.yml`
  is a case; a directory yields a suite result).
- `--tag <t>` — only run cases whose `tags` include every `--tag` (repeatable, AND).
- Global: `--config-dir <path>` (sets `AIDEV_CLIS_HOME`, passed to every
  subprocess), `--pretty`, `-v`/`-vv`.

tcli has **no config file of its own**. Capabilities map to apps/targets declared in
the respective `apicli.yaml` / `dbcli.yaml` / `logcli.yaml`. Child binaries are
found next to the `tcli` executable first, then on `PATH`.

## Case file

A case **declares its capabilities up front**, then runs four ordered phases.
See `examples/tcli-case.yaml` for a fully-commented case.

```yaml
name: order service post-deploy gate
tags: [smoke, orders]
apps:                              # api: alias -> identity (app defaults to the alias)
  buyer: { app: orders,  actor: qa }
  admin: { app: billing, actor: qa_admin }
dbs:   { main: orders_mysql, cache: orders_redis }   # alias -> dbcli target
logs:  { svc: orders_logs }                          # alias -> logcli target
vars:   { customer: "C-1001" }     # plus built-ins {{run_id}} {{uuid}} {{now}}
safety: { allow_db_write: false }  # write SQL refused unless true (+ step.write)
setup:      [ ... ]                # runs first; failure stops the case
steps:      [ ... ]                # main flow; stops at first failure
assertions: [ ... ]                # standalone checks over captured vars
cleanup:    [ ... ]                # always runs, best-effort
```

**Declare-before-use.** Every step picks its capability with `target: <alias>`
into `apps`/`dbs`/`logs`. When a section has exactly one entry, its steps may
omit `target`. A target that isn't declared (or an omitted target with multiple
entries) is a validate error — so the top of the case is the full list of what it
touches.

**Phases.** `setup` failure skips `steps` and `assertions`; a `steps` failure
skips `assertions`; `cleanup` **always** runs and never changes the verdict (a
cleanup step whose template vars are unset is skipped, not errored). `vars` are
shared across phases; built-ins are seeded once per run.

## Actions

Each phase is a list of actions; an action is exactly one of `api`, `db`, `log`,
or a standalone assertion (only `expect:`).

```yaml
# api -> apicli call (the request: block is parsed into method/path/headers/body)
- name: create order
  api:
    target: buyer
    request: |
      POST /api/v1/orders
      Content-Type: application/json
      X-Run-Id: {{run_id}}

      {"customer":"{{customer}}"}
    expect:  [ "status_code == 200", "body.code == 0", "body.data.id exists" ]
    capture: { id: body.data.id }
    save_body: /tmp/out.xlsx        # optional -> apicli --output-file (no body to assert then)
    wait: { timeout: 30s, interval: 2s }
    assert_script: { command: sh, args: ["-c", "test -s /tmp/out.xlsx"], timeout: 15s }

# db -> dbcli <driver> --target <target> [--allow-write] "<statement>"  (driver auto-discovered)
# A case may span drivers — name a different target per step. `sql` is the dbcli
# statement for that target's driver: SQL, a redis command, or a mongosh statement.
- name: verify row
  db:
    target: main
    sql: "SELECT status FROM orders WHERE id = '{{id}}'"
    write: false                    # true requires safety.allow_db_write (else SAFETY_BLOCKED)
    expect:  [ "count == 1", "rows.0.0 == confirmed" ]
    capture: { db_status: rows.0.0 }
- name: verify doc (mongo find -> document ARRAY at the payload root)
  db: { target: docs, sql: 'db.orders.find({_id:"{{id}}"})', expect: [ "count >= 1", "0.status == confirmed" ] }

# log -> sls trace/search, or non-sls logs + client-side grep  (adapter auto-discovered)
- name: correlate in logs
  log:
    target: svc
    trace: "{{corr}}"               # OR query: "<expr>"; empty/undefined needle -> step SKIPPED
    from: 15m
    expect: [ "count >= 1" ]

# standalone assertion (no backend): expressions over captured vars
- name: api matches db
  expect: [ "{{db_status}} == confirmed" ]
```

## expect — one expression list

Every step (and every standalone assertion) carries `expect:`, a list of
expressions. There are no assertion "keys" to memorize — just `<lhs> <op> <rhs>`:

| operator | meaning |
|---|---|
| `path == v` | equal (numeric when both parse as numbers, else string) |
| `path != v` | not equal |
| `path contains v` | substring (quote values with spaces: `contains "order created"`) |
| `path not contains v` | substring absent (exact negation of contains; a missing path passes vacuously). On an object/array path the value checked is the raw JSON serialization — so `body not contains <plaintext>` asserts the plaintext appears nowhere in the serialized body (desensitization check that survives field moves) |
| `path matches re` | Go regex (RE2), unanchored partial match — write `^...$` yourself; an invalid regex fails `validate` |
| `path is t` | JSON type assertion, `t` ∈ string \| number \| bool \| array \| object \| null (catches type regressions `==` cannot, e.g. a snowflake ID no longer serialized as a string); result paths only, not standalone assertions |
| `path exists` | present and non-empty (no rhs) |
| `path not exists` | missing or empty (exact negation of exists; no rhs) |
| `path >= n` (`>` `<=` `<`) | numeric comparison |
| `count >= n` (`==` …) | length of the step's result collection |

- `path` is **result-relative** (no `data.` prefix): api → `status_code`,
  `body.code`, `headers.X-Trace-Id`, `trace_id`; db (SQL/redis) → `rows.0.0`,
  `columns.0`; db (mongo) / log → `0.field` (document/record array).
- `count` is the collection length: `body` (api), `rows` (SQL/redis), the root
  array (mongo find / log).
- In a standalone assertion the expression is `{{}}`-rendered first and both
  operands are literals. `{{vars}}` are rendered everywhere before evaluation.
- A malformed expression fails `validate` (`EXPECT_INVALID`).
- `is` gotcha: dbcli deliberately emits int64 beyond ±2^53 as a JSON string
  (JS-precision protection), so on a `db` step a big snowflake ID is a string
  regardless of the column type — assert ID type contracts on the `api` step,
  where the service's own serialization reaches the expression untouched.

## capture, templating, built-ins

`capture: { var: <path> }` stores a result-relative value into `vars` for later
steps/assertions. `{{var}}` substitutes from `vars` + captures + built-ins; an
undefined variable is `TEMPLATE_VAR_MISSING` (exceptions: a `log` needle or a
`cleanup` step with an unset var SKIPs, not errors). Built-ins seeded once per
run: `{{run_id}}` (unique — mint collision-free ids), `{{uuid}}`, `{{now}}`.

## Identity

An `apps` entry's `actor` is a **name** (in `actors.yaml`) or an **inline** actor
for an account not in `actors.yaml`:

```yaml
apps:
  buyer: { app: orders, actor: qa }                         # by name
  adhoc: { app: orders, actor: { vars: { username: u, password: p } } }   # inline
```

Inline vars are rendered, written to a temp `{vars:{...}}` file named by a stable
hash (same creds reuse the same session across runs), and removed when the run
ends. tcli never logs in itself — apicli captures/relogs the session; an auth
failure surfaces with an `apicli login <app> --actor <name>` hint.

## wait / retry, trace, safety

- **wait:** `wait: { timeout, interval }` retries a step until its `expect`
  passes or the timeout elapses (defaults 30s / 2s when present; single attempt
  otherwise). Retries fire only on assertion failures and retryable
  remote/timeout errors; config/auth/safety failures stop immediately.
- **trace = grep:** a `log` step's needle (`trace:` exact id, or `query:`
  expression) is any captured value. After each `api` step tcli auto-captures
  `data.trace_id` into `{{trace_id}}` *when the app sets a `trace_field`* — not
  load-bearing; an empty/undefined needle just SKIPs. On `sls` targets `trace:` uses
  the indexed `logcli sls trace` (full-text fallback) and `query:` uses `search`;
  other adapters fetch a bounded `logcli <adapter> logs` slice and grep `message`.
- **safety:** a `db` step with `write: true` requires `safety.allow_db_write` or
  tcli returns `SAFETY_BLOCKED` before calling dbcli; dbcli's own guard enforces
  it again (defense-in-depth). tcli never reclassifies SQL.

## Report (conclusion-first)

`run` emits `{"data": <CaseResult>}` with the verdict and root cause **first**,
then per-phase detail:

```jsonc
{ "data": {
  "verdict": "PASS|FAIL|ERROR|SAFETY_BLOCKED",
  "case": "...", "run_id": "tcli-…",
  "summary": "FAIL at steps 'verify row': count == 1 (got 0)",
  "failure": { "phase","step","type","category","code","message",
               "evidence": { /* last response, secrets redacted + truncated */ },
               "next": [ { "command": "apicli call orders /… -X GET --actor qa --output json", "reason": "…" } ] },
  "counts": { "total","passed","failed","skipped" },
  "setup": [], "steps": [ /* name,type,status,attempts,elapsed_ms,last */ ],
  "assertions": [], "cleanup": []
}}
```

A directory yields a `SuiteResult` surfacing the first failing case. Evidence
snapshots redact `password`/`token`/`authorization`/`secret`/`cookie` keys and
truncate long strings.

## Discovery & isolation

tcli runs `dbcli targets` / `logcli targets` once per run (cached) to map a target to its
driver/adapter, so a case names only the target. It links **no** sibling CLI's Go
package — the repo's isolation guard (`make check`) lists `tcli` and fails the
build on any `internal/apicli|dbcli|logcli` import. See `docs/ADAPTER-ISOLATION.md`.

## Output contract

Success `{"data": <payload>}`; error `{"error":{"code","message"}}`. A *ran* gate
that fails is still a `{data}` envelope (verdict FAIL/ERROR) with a non-zero exit,
mirroring apicli's business-failure convention — CI keys off the exit code: 0
PASS, 1 FAIL/ERROR, 2 SAFETY_BLOCKED/invalid. `-v`/`-vv` add a `diagnostics`
array; `--pretty` indents. See `docs/OUTPUT-CONTRACT.md`.
