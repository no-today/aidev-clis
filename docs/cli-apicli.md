# apicli

apicli gives an AI agent authenticated HTTP access to internal apps without
exposing the credential. The CLI holds the session, injects it on every call,
returns a `{data}` / `{error}` envelope, and audits every invocation. The remote
app's own authorization is the boundary.

apicli earns its place where raw `curl` cannot: apps that require browser
login, cookie capture, or multi-step token auth.

## Synopsis

```
apicli {call|login|whoami|logout} <app> [path] [--actor <name>] [--env <name>] [flags]
```

The subcommand leads; `<app>` is its first positional. There is no `--app` flag.

```sh
apicli call   svc-login /api/me --actor alice
apicli call   svc-login /api/order --actor alice --env pre -X POST \
              -H 'Content-Type: application/json' -d '{"x":1}'
apicli call   svc-login /api/x --actor alice --curl      # print equivalent curl, no execute
apicli login  svc-login --actor alice
apicli whoami svc-login --actor alice
apicli logout svc-login --actor alice
```

Global flags: `--pretty` (pretty-print JSON output); `--config-dir <path>`
(override the config root `~/.aidev-clis` by setting `AIDEV_CLIS_HOME` before any
config/credential load — parity with dbcli/logcli, lets an orchestrator pin a
shared config root).

> Intentionally **not** provided: `--json`, `--credential`, and
> `--output table|plain`. A usage audit found zero real executions — agents always
> use `-d` with JSON bodies and consume the JSON envelope. The `--credential`
> *mechanism* lives on as `secret:<name>` (see Security model).

## Addressing — three axes

| Axis | Flag / position | Meaning | Default |
|---|---|---|---|
| **app** | 1st positional after the verb (required) | which endpoint: its auth flow + base URL | — |
| **actor** | `--actor <name>` | which account | app's `default_actor` in `apicli.yaml` |
| **env** | `--env <name>` | named environment of this app (base-URL override) | app's `base_url` |

- `--env` resolves within the chosen app's `envs:` map — it is not a cross-CLI
  concept. dbcli / logcli / jcli use `--target` for their own selection axis,
  independently (apicli keeps `--env` because an app genuinely has multiple
  environments).
- `--base-url <url>` overrides the base URL for one call (ad-hoc escape hatch).
  `--env` and `--base-url` are mutually exclusive; passing both is an error.

## Subcommands

| subcommand | description |
|---|---|
| `call <app> <path>` | send an HTTP request using the active session |
| `login <app>` | authenticate and capture a session |
| `whoami <app>` | inspect the current session (local, no network) |
| `logout <app>` | remove the stored session |

### call flags

All flags below are on `apicli call`. Addressing flags (`--actor`, `--env`,
`--base-url`) apply to all four verbs.

| flag | short | default | description |
|---|---|---|---|
| `--request` | `-X` | `GET` | HTTP method |
| `--header` | `-H` | — | request header, repeatable |
| `--data` | `-d` | — | request body (raw passthrough, no re-encoding) |
| `--output` | — | `json` | `json` (envelope) or `raw` (body only) |
| `--output-file` | — | — | stream the response body to this file (binary-safe) |
| `--headers-file` | — | — | write `{status_code, url, headers}` JSON to this file |
| `--connect-timeout` | — | `30s` | request timeout; `0` (unset) falls back to 30s |
| `--allow-cross-origin` | — | false | permit sending the session to a host other than the app base |
| `--curl` | — | false | print equivalent curl command with injected auth, don't execute |

Body is passed through raw — `-d 'a=1&b=2'` with a form `Content-Type` reaches
the server byte-for-byte. No implicit JSON re-encoding.

### File downloads — `--output-file` / `--headers-file`

A binary/export response (xlsx/pdf/zip) **must not** go into the JSON envelope —
inlining binary into `data.body` corrupts and bloats it. With `--output-file`,
the body is streamed to disk (never held whole in RAM — exempt from the in-memory
cap) and the envelope carries **metadata only**, `body` stays absent:

```sh
apicli call shop /api/export --actor demo \
  --output-file /tmp/x.xlsx --headers-file /tmp/x.headers.json
```

```json
{"data": {
   "status_code": 200, "ok": true,
   "body_file": "/tmp/x.xlsx",
   "body_bytes": 20480,
   "content_length": 20480,
   "stream_complete": true,
   "sha256": "9f86d0…",
   "headers_file": "/tmp/x.headers.json"
}}
```

- `body_bytes` — bytes actually written to disk. `content_length` — the
  server-declared length (`-1` if the response had no `Content-Length`).
  `stream_complete` — the body read finished without error.
- **Empty-vs-failure disambiguation.** These three together let an assert tell a
  genuinely empty 200 from a capture failure: `content_length == 0 &&
  stream_complete` → upstream exported nothing; `body_bytes < content_length` or
  `!stream_complete` → the stream was truncated / failed.
- `--headers-file` writes `{status_code, url, headers}` — so a script can read
  `Content-Disposition` (filename), `Content-Type`, or a header-borne trace id
  even though the body never enters the envelope.

### Inline actor — `--actor-file`

`--actor <name>` resolves an account from `actors.yaml`. `--actor-file <path>`
instead supplies a **complete** one-off actor inline (a JSON/YAML `{vars:{...}}`
doc) for an ad-hoc identity that should not live in the shared library. It does
**not** merge with an `actors.yaml` account of the same name. Available on
`call`/`login`/`whoami`/`logout`.

### Per-login one-time values — `--var` (login only)

`apicli login <app> --var k=v` (repeatable) supplies a per-invocation value that
cannot be a static `actors.yaml` default — e.g. an OTP / `verifyCode`. Most logins
use `vars_required` from the actor record and never pass `--var`. It is on `login`
only (never `call`). A `--var` value visible in argv carries a sensitive-arg warning.

> **`--curl` redacts the credential.** The emitted curl shows the injected auth
> header's *shape* only (e.g. `Authorization: Bearer <token>`), never the live
> value — so a shared or logged `--curl` line doesn't leak the token. It is a
> local debugging aid for the request shape, not a runnable authenticated call.

## Session lifecycle

Sessions are keyed per `(app, actor, env)` and stored at
`~/.aidev-clis/sessions/<app>/<actor>/<env>` (mode 0600). Multiple accounts and
multiple environments coexist without collision.

`apicli call` auto-detects expiry via the app's `expired_when` predicate and
re-logins once, then retries — without the agent having to do a `whoami` first:

1. Send the request with the stored session.
2. If `expired_when` matches **and** all `vars_required` are resolvable from the
   actor record → **re-login once, retry once**.
3. If retry succeeds → return `{data}` with `warnings: ["session re-established"]`.
4. If the flow needs interaction (captcha / SMS) or retry still fails → return
   `{error: {code: "SESSION_EXPIRED", ...}}` — run `apicli login <app>` explicitly.

`whoami` is a local read (no network) — useful for a quick sanity check, but with
auto-relogin in `call` the agent should rarely need the whoami-then-call dance. It
returns `logged_in`, `age_seconds`, and:

- `expiry_risk` — a bucket on the session age: `low` (≤8h) / `med` (≤24h) /
  `high` (older). A coarse hint at whether the next call may need a relogin.
- `captured_vars` — the **keys** captured by the login flow (e.g. `["token"]`),
  never their values, so the agent can confirm what the session holds.

### Failed-call trace hint — `log_env`

When a call comes back not-OK and a `trace_id` was resolved (via `trace_field`),
apicli writes a one-line pivot hint to **stderr** (not the JSON envelope):

```
[apicli] traceId: abc123 → logcli sls trace abc123
```

If the app sets `log_env`, it is appended as `--target <log_env>`, giving the agent a
ready-to-run logcli command to pull the matching server logs.

## Response interpretation — per-app predicate engine

These apps cannot be judged by HTTP status alone: many return `200` always and
embed the real outcome in `body.code` / `body.status`. apicli has a small
declarative predicate engine, configured per app under `response:`:

- `ok_when` — call succeeded at the business level. Drives the `ok` flag and exit
  code.
- `expired_when` — session is dead. Checked first; triggers auto-relogin.

Predicate grammar:

- Left side (the signal source): `status` (HTTP int), `body.<gjson-path>` (e.g.
  `body.code`, `body.data.status`), or `header.<name>`.
- Operators: `==`, `!=`, and `||` between clauses. Nothing else.
- Right side: a string/int literal, or `null` (for existence: `body.token != null`
  means present, `body.error == null` means absent).

apicli **classifies, it does not transform** — the full body is always returned
verbatim; these predicates only set `ok`, the exit code, and the relogin trigger.

## Output

A reachable response is always `{data}` (never `{error}`) — even a business
failure is a real answer the agent must read. The `ok` flag is added from
`ok_when`:

```json
{"data": {"status_code": 200, "ok": false, "headers": {...},
          "body": {"code": 4001, "msg": "余额不足"}}}
```

`{error}` is reserved for transport / config / auth faults where there is no
useful body:

```json
{"error": {"code": "SESSION_EXPIRED", "message": "..."}}
```

### Exit codes

| code | class | description |
|---|---|---|
| 0 | OK | `ok_when` true |
| 1 | general / business-fail | reachable but `ok_when` false — body still in `{data}` |
| 2 | config | unknown app / actor / env, bad YAML |
| 3 | auth | `SESSION_EXPIRED` / auth could not be established |
| 4 | timeout | request timeout |
| 5 | remote | transport error (DNS / connect) |

## Config

Two files, clean layers:

### `~/.aidev-clis/apicli.yaml` — per-app auth flow, base URL, envs

```yaml
apps:
  svc-login:
    base_url: http://uat.example.com      # default env
    envs:
      pre:  http://pre.example.com
      prod: https://prod.example.com
    default_actor: alice
    auth:
      kind: flow                      # flow | none
      vars_defaults: { orgId: "999" }       # lowest-precedence fallbacks
      vars_required: [phoneNo, password]    # supplied by the actor record
      flow:
        - request: |
            POST /auth/login
            Content-Type: application/json

            {"phoneNo":"{{phoneNo}}","password":"{{password}}"}
          assert: "body.code == 0"          # false → login fails AUTH_FAILED
          capture:
            token:       body.data.token
            accessToken: header.Access-Token
      inject:                               # how the captured session rides on calls
        headers:
          Authorization: "Bearer {{token}}"
          Access-Token:  "{{accessToken}}"
    response:
      ok_when:      "body.code == 0"
      expired_when: "body.code == 401"
  svc-open:
    base_url: https://svc-open-uat.example.com
    auth: { kind: none }
    response:
      ok_when:      "status == 200"
      expired_when: "status == 401 || body.status == 'NOT_LOGIN'"
```

Config fields:

| field | description |
|---|---|
| `apps.<name>.base_url` | default base URL for this app |
| `apps.<name>.insecure_skip_verify` | skip TLS cert verification (internal HTTPS with self-signed certs) |
| `apps.<name>.ca_cert` | path to a PEM CA bundle for internal HTTPS (private CA) |
| `apps.<name>.envs` | named URL overrides (selected with `--env`) |
| `apps.<name>.default_actor` | actor used when `--actor` is not specified |
| `apps.<name>.auth.kind` | `flow` or `none` |
| `apps.<name>.auth.vars_defaults` | lowest-precedence fallback vars (see precedence below) |
| `apps.<name>.auth.vars_required` | variables the actor must supply for the login flow |
| `apps.<name>.auth.flow[].name` | optional step label; shown in a failed-assert error (else `step N`) |
| `apps.<name>.auth.flow[].request` | raw HTTP template for one login step (`{{var}}` filled from vars + earlier captures) |
| `apps.<name>.auth.flow[].assert` | per-step predicate that must hold post-step, else login fails (see below) |
| `apps.<name>.auth.flow[].capture` | var → capture source: values to extract from the response (see below) |
| `apps.<name>.auth.flow[].cookie_from_set_cookie` | capture this step's `Set-Cookie` into the session cookie (see below) |
| `apps.<name>.auth.inject.header` | single header to inject on every call (e.g. `Authorization: Bearer {{token}}`) |
| `apps.<name>.auth.inject.headers` | map of name → template, each injected on every call (see below) |
| `apps.<name>.auth.inject.cookie` | cookie to inject on every call |
| `apps.<name>.response.ok_when` | predicate for business success |
| `apps.<name>.response.expired_when` | predicate for session expiry |
| `apps.<name>.extra_headers` | per-app constant headers merged into every request (a per-call `-H` of the same name overrides) |
| `apps.<name>.trace_field` | where the trace id lives; emitted as `data.trace_id` |
| `apps.<name>.log_env` | logcli env name appended to the failed-call trace hint (see below) |

#### Login flow — capture, assert, cookies, inject, vars

The flow runs each step in order, accumulating captured vars; a later step's
`{{var}}` resolves from the vars **and** anything captured in earlier steps, so a
multi-step handshake (nonce → ticket → token) works end-to-end.

- **`capture`** — `var → source`. Sources: `body.<gjson>` | `header.<name>` |
  `status`. A bare `<x>` is shorthand for `body.<x>`. `header.<name>` is matched
  case-insensitively (`header.access-token` == `header.Access-Token`). A capture
  that resolves empty fails the login (a wrong-password 200 leaves the token `""`).
- **`assert`** — a predicate (same grammar as `ok_when`, e.g. `body.code == 0`)
  evaluated after the step. A false assert fails login with `AUTH_FAILED` plus a
  short body excerpt, so a bad-credentials response is surfaced clearly.
- **`cookie_from_set_cookie: true`** — captures the step's response `Set-Cookie`
  (name=value pairs, attributes dropped) into the session cookie, which is then
  replayed as a `Cookie` header on every call. Use for pure cookie/session apps
  (e.g. `JSESSIONID`) that capture nothing from the body.
- **`inject.headers`** — a map of header name → template rendered from the session
  vars (`Authorization: "Bearer {{token}}"`). All entries are injected on every
  call. A constant value (no `{{...}}`) is injected as-is. A per-call `-H` with the
  same name overrides an injected header.
- **`vars_defaults`** and precedence — defaults are the lowest-priority source.
  The precedence is `vars_defaults < actor / CLI vars < captured`: an actor record
  (or `--var`) beats a default, and a value captured during the flow beats both.

#### `extra_headers`

```yaml
apps:
  shop:
    extra_headers:
      Client-Id: demo_app_a
```

Mandatory constant headers (e.g. `Client-Id`) some legacy endpoints require. They
are merged into every request for the app; a per-call `-H` with the same name wins.

#### `trace_field`

Where the response carries its trace id. apicli resolves it and emits the value as
`data.trace_id`. Grammar (tiny): `header.<name>` | `body.<dotpath>` | bare `<x>`
(≡ `body.<x>`). Missing / oversized / non-JSON → omitted.

```yaml
apps:
  shop:
    trace_field: header.X-Trace-Id      # or: body.data.traceId
```

> `trace_field: header.<name>` is matched case-insensitively. Prefer a `header.`
> source for export endpoints, since an xlsx body has no JSON to mine.

### `~/.aidev-clis/actors.yaml` — account library

```yaml
actors:
  svc-login:
    alice: { phoneNo: "13800000000", orgId: "999" }
    bob: { phoneNo: "13800000001", orgId: "999" }
```

Layer rule: **apicli.yaml owns app auth + base URL; actors.yaml owns accounts;
the caller may override `--actor` / `--env`.** No fourth layer.

## Security model

The remote app's own authorization is the primary boundary. apicli only manages
session capture and header injection; it does not filter or restrict which
endpoints the agent may call. Operator responsibility: provision an account with
the minimum role the agent needs.

Session files are stored at mode 0600. Credential values (passwords, raw tokens)
are never written to disk — only captured session artifacts (tokens, cookies) from
the flow response.

### Credential indirection — `secret:<name>`

An actor var value of the form `secret:<name>` is **not** stored in config: it
resolves at use-time from `~/.aidev-clis/credentials/<name>` (permission-checked).
Plain values still work for dev/throwaway; `secret:<name>` is the path for anything
real, keeping secrets out of `actors.yaml`.

```yaml
actors:
  shop:
    demo: { username: "demo@x.test", password: "secret:shop_demo_pw" }
```

### Guards

- **Cross-origin URL guard.** A call whose resolved URL host differs from the app's
  configured base/env host is refused with `API_CROSS_ORIGIN_URL` (config class) —
  the injected session is not silently carried to a foreign host. Pass
  `--allow-cross-origin` to override. (A `--base-url` to a declared env of the same
  app is fine.)
- **Response size cap.** The in-memory body read is capped at 64 MiB; past the cap
  the call returns `{error: RESPONSE_TOO_LARGE}`. The `--output-file` streaming path
  is exempt — that is exactly what it is for.

Private IP addresses and plain HTTP are allowed. apicli is used against dev VMs,
bastion hosts, and in-cluster pod IPs where TLS and public routing are not available.

## Audit

Every invocation — including `--curl` previews — appends to the current day-file
`~/.aidev-clis/audit/<YYYYMMDD>.jsonl` (30-day auto-prune).
Non-GET calls write a `started` line before dispatch and a terminal line after,
sharing an `id`, so a kill still leaves a trace.
Credential values are never written.
