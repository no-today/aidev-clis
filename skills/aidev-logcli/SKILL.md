---
name: aidev-logcli
description: Use when an AI agent needs to read logs — listing what's tailable (apps/containers/files/logstores), fetching a bounded slice of logs, following live (-f / tail), searching, or tracing a request across services — from kubectl, docker, SSH hosts, local files, or Aliyun SLS, across configured targets.
---

# logcli

`logcli` gives an AI agent controlled, audited read access to logs. The adapter
injects a scoped credential you never see, runs the native backend, and returns a
JSON envelope. Six adapters:

```
logcli <adapter> [--target <name>] [--timeout <dur>] [--pretty] <args...>
logcli targets                    # list configured targets (no --target, no backend)
```

| adapter | reads | discovery |
|---|---|---|
| `kubectl` | `logs` (pass-through `kubectl logs`) | `ls` → deployments + app labels |
| `docker` | `logs <container>` | `ls` → `docker ps` |
| `ssh-file` | `logs` (`--tail N`, `-f`) over SSH, configured files only | `ls` → the configured files |
| `ssh-docker` | `logs <container>` over SSH | `ls` → `docker ps` |
| `sls` | `search` / `trace` / `tail` (signed GetLogs) | `doctor` → verify config (no `ls`: logstore is fixed in config) |
| `local-file` | `logs` (`--tail N`, `-f`) over configured files | `ls` → the configured files |

`<adapter>` must match the target's `adapter` (else `ADAPTER_MISMATCH`).

**Every adapter also has `doctor`** — a staged connectivity probe (`config` →
`connect`) that returns `{"data":[{"name","ok","detail"},...]}` and exits non-zero
if a stage fails. Run it to confirm a target is reachable before relying on it:
`logcli kubectl --target uat doctor` (probes `kubectl version`), docker `docker
version`, ssh-* the SSH connection, `sls` a tiny `GetLogs`, local-file that
the files are readable.

## Always `ls` first (you can't guess names)

You cannot tail an app/container you haven't discovered. Start with the
adapter's `ls` (cheap, finite) to learn the selector, then read:

```
logcli targets                                 # which targets exist
logcli kubectl --target uat ls                 # → [{name, app}] — the app= labels
logcli kubectl --target uat logs -l app=pbs-ca --tail 200
```

Exception: `sls` has no `ls` — its project + logstore are fixed in the target
config. Run `doctor` instead to confirm that config is correct and reachable
before querying.

## Output contract (parse this)

- **Finite read** (`logs` without `-f`, `search`, `trace`, `ls`) → one envelope:
  `{"data":[ <record>, ... ]}` (+ optional `"warnings"`), or `{"error":{"code","message"}}`.
- **Live stream** (`-f`/`--follow`, or `sls tail`) → **NDJSON**, one JSON
  object per line, discriminated by `type`:
  ```
  {"type":"record","data":{...}}
  {"type":"record","data":{...}}
  {"type":"end","count":2}                  // or {"type":"error","error":{...}}
  ```
  Pass-through adapters wrap each line as `{"message": "<log line>"}`; `sls`
  emits the structured log fields.
- JSON/NDJSON envelope by default, which is already what an agent wants — **don't
  pass `--output`**. (`--output raw` drops the envelope to one verbatim log line per
  record, and `--pretty` indents — both are for humans piping/reading, not agents.)
- SLS records carry heavy metadata (`__tag__:*`, `_container_*`, pack ids). Instead
  of piping through a filter script, project server-side-shaped output with
  `--fields message,__time__` (any comma-separated field list) — unlisted fields
  are dropped, plain-line records pass through.
- Always pass `--target` explicitly. With multiple targets configured, an omitted
  `--target` silently uses the default target — the envelope warns when this
  happens; treat that warning as a bug in your invocation. The legacy `--env` flag
  errors with `LEGACY_FLAG` (renamed to `--target`).
- Log lines are a PII exit: mask real PII (id numbers, phones, card numbers,
  names) before quoting a log snippet into docs, tests, or persistent notes —
  full rules in the **aidev-dbcli** skill.

## Bounded vs live — pick deliberately

- For **evidence / a snapshot** (the usual agent need), use a **finite** read:
  `kubectl logs --tail 200`, `sls search ... --size 100`. You get one
  envelope and you're done.
- `-f` / `--follow` / `sls tail` **runs until killed** — only for watching
  live, and **always bound it with `--timeout`** (e.g. `--timeout 30s`), or it
  never returns.

## Per-adapter cheat sheet

Pass-through adapters (`kubectl`/`docker`/`ssh-*`) forward the backend's native
flags — `-l`/`--selector`, `--tail`, `--since`, `-c <container>`, `-f` all work.
Only auth/context-override flags are denied — the complete deny set per adapter:
`--kubeconfig`/`--server`/`--token`/`--context`/`--as`/`--user`/`--cluster` for
kubectl; `--host`/`-H`/`--config`/`--context`/`--tlscacert` for docker and
ssh-docker. The pinned credential is the boundary.

```
logcli kubectl    --target uat logs -l app=pbs-ca --tail 200          # finite
logcli kubectl    --target uat logs -f -l app=pbs-ca --timeout 30s    # live
logcli docker     --target box ls
logcli docker     --target box logs --tail 100 my-container
logcli ssh-file   --target vps logs --tail 200                        # tails configured files only
logcli ssh-docker --target vps logs my-container
logcli local-file --target app logs -f --tail 100                     # local files, Go-native follow
```

**sls** has its own query verbs (not pass-through):

The verb's primary subject is a positional (the verb already names it — no
`--query`/`--trace-id`):

```
logcli sls --target prod doctor                              # verify config + reachability
logcli sls --target prod search "level: ERROR" --from 1h --to now --size 100
logcli sls --target prod --from 1h --to now search "level: ERROR" --size 100
logcli sls --target prod trace  abc123 --from 24h            # one request across services
logcli sls --target prod tail   "*" --interval 5s --timeout 30s
```

- The query positional is SLS query syntax (defaults to `*`); `--from`/`--to`
  accept `now`, `30s`/`5m`/`2h`/`1d`, unix seconds, or RFC3339. `search` caps at
  `--size` (default 100). SLS modifier flags may appear before or after the
  verb.
- **`field:value` and `field: value` are equivalent** — whitespace around the
  colon is optional (verified against the GetLogs API). What actually matters is
  that the field is **indexed** as key-value: querying an unindexed field returns a
  hard `ParameterInvalid` ("key (X) is not config as key value config") — it does
  NOT silently fall back to full-text. Quote values with spaces/`:`/special chars:
  `msg: "connection failed"`.
- `trace` matches the env's `trace_field` (default `traceId`) — the go-to for
  "follow request X across services".
- `doctor` reports staged checks (`config` → `connect`) like jcli/dbcli: `data`
  is a flat `[{name,ok,detail}]` array, health is the exit code (no top-level
  `ok`). `connect` issues the same signed GetLogs as search against the configured
  logstore — the go-to "is this target set up right / am I authorized" probe when a
  query errors or returns nothing.

### SLS query syntax (the `<query>` positional)

A query is `<search> | <analysis>`: the search half filters, the optional SQL half
after `|` aggregates. Search alone is fine; analysis always needs a search (`*` = all).

Search half (before `|`):
- Full-text: a bare term matches any field — `timeout`. Multiple bare terms are
  **AND**, order-independent (`abc def` = has both). For an exact phrase, prefix `#`
  and keep the quotes: `#"connection refused"` (can't combine with a `|` analysis).
- Field match: `level: ERROR` — the field must be indexed; `field:value` and
  `field: value` are equivalent (space optional).
- Boolean (case-insensitive) `and` `or` `not`; `()` groups whole conditions —
  `level: ERROR and status: 500`, `(app: a or app: b) and env: prod`,
  `not path: /health`. To OR several values of one field, repeat the field:
  `app: a or app: b` (the shorthand `app: (a or b)` is NOT valid in the search box).
- Numeric compare (long/double fields): `latency > 50`, `status >= 500`, `status = 200`.
- Numeric range, closed interval, `*` = unbounded: `status: [200, 299]`,
  `latency: [100, *]`, `bytes: [*, 1024]`.
- Wildcards: `*` = 0+ chars, `?` = 1 char — `host: web*`, `uri: /api/v?`; match a
  literal `*`/`?` by escaping (`\*`, `\?`).
- Field exists / absent: `field: *` / `not field: *`.
- Quote any value with chars beyond CJK/letters/digits/`_`/`-`/`*`/`?`:
  `url: "/a/b?c=1"`, `msg: "connection refused"`.

Analysis half (after `|`, standard SQL over the matched rows; the field must have
index statistics enabled):
- String literal = single quotes; bare or double-quoted = a column. `'status'` is
  the text `status`; `status` / `"status"` is the field. `__time__` = reserved
  unix-seconds time column.
- Top-N: `* | select status, count(*) as c group by status order by c desc limit 20`
- Time series: `* | select date_trunc('hour', __time__) as t, count(*) as pv group by t order by t`
- Filter then aggregate: `level: ERROR | select count(*) as errors`
- With `group by`, `select` may only list grouped columns or aggregates.

## Infer the target first

`logcli targets` is a safe local read; use it to list candidates (name + adapter +
description). Prefer an explicit user target or the release/UAT/dev context. If
several fit or the user is vague ("prod logs"), ask one short confirmation before
hitting a backend.

Target resolution is adapter-aware: if the adapter has **exactly one** configured
target, `--target` is optional (auto-selected); with several, omitting `--target` returns
`TARGET_AMBIGUOUS` listing the candidates — pick one explicitly.

## Config (see docs/cli-logcli.md for full per-adapter fields)

```yaml
# ~/.aidev-clis/logcli.yaml
targets:
  uat:
    adapter: kubectl
    description: UAT cluster
    kubeconfig_credential: k8s.uat   # a LOGS-ONLY ServiceAccount kubeconfig (0600)
    namespace: uat                   # pinned; a user --namespace can't escape it
  prod:
    adapter: sls
    description: prod app-service logs   # shown by `logcli targets` — what this target is for
    project: app-prod
    logstore: app-log
    endpoint: cn-hangzhou.log.aliyuncs.com   # bare region host, not a console URL
    credential: sls.ak               # AK/SK JSON, RAM grants only log:GetLogStoreLogs
```

Credentials live in `~/.aidev-clis/credentials/` (0600) and never reach the AI.
The credential's own scope (logs-only SA, minimal RAM policy) is the real
boundary; the adapter allowlist is defense-in-depth.

The bundled `logcli.yaml` is a reference template — copy it to `~/.aidev-clis/logcli.yaml`
and fill in your targets/credentials (real configs never leave the machine). You
can offer to write it for the user from this sample.

For the shared `~/.aidev-clis` layout, the credentials model, and from-zero setup
steps, see the **use-aidev** skill.

## Don't use logcli for

- Writing/deploying — it's read-only. Deploy via `jcli`, change state via `apicli`.
- Bulk log export / long archival pulls — use the backend's native export.
- Metrics/traces dashboards — logcli reads log lines, not time-series.
- An assertion you want kept + re-runnable with a PASS/FAIL verdict — a post-deploy gate or a cross-system regression invariant — use `tcli`.

## Audit

Every invocation (including rejected ones) appends to `~/.aidev-clis/audit/<YYYYMMDD>.jsonl`
(30-day auto-prune) with the adapter, target, full command, and outcome (`ok`/`error`).
Credentials are never written.
