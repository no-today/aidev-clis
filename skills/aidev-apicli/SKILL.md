---
name: aidev-apicli
description: Use when managing aidev HTTP session state or making HTTP calls with apicli login (capture a session via a flow), apicli whoami (inspect current session), apicli logout (remove session file), or apicli call (send HTTP request with injected session). Covers verb-first addressing, auto-login + auto-relogin (call is self-sufficient — no login-first), the {data}/{error} envelope, per-app response predicates, actors (incl. secret:<name>), and flow fidelity — multi-step + cross-step capture, per-step assert, cookie_from_set_cookie, multi-header inject, vars_defaults, extra_headers, trace_field, and file downloads (--output-file).
---

# apicli

`apicli` gives an AI agent authenticated HTTP access to internal apps. The CLI
holds the session, injects it, runs the request, and returns a minimal JSON
envelope. You never see the credential; the remote app's own authorization
decides what the agent may do.

```
apicli call   <app> <path> [--actor <name>] [--env <name>] [-X method] [-H header] [-d body] [--connect-timeout dur] [--output json|raw] [--curl] [--pretty]
apicli login  <app>        [--actor <name>] [--env <name>]
apicli whoami <app>        [--actor <name>] [--env <name>]
apicli logout <app>        [--actor <name>] [--env <name>]
```

The subcommand always leads; `<app>` is its first positional. There is no
`--app` flag and no global `--env`.

## Addressing — three axes

| axis | flag / position | default |
|---|---|---|
| **app** | 1st positional after the verb (required) | — |
| **actor** | `--actor <name>` | app's `default_actor` in `apicli.yaml` |
| **env** | `--env <name>` | app's `base_url` |

`--env` resolves within the chosen app's own `envs:` map. `--base-url <url>`
overrides the base URL for a single call (mutually exclusive with `--env`).

## Typical flow — just `call`, never login-first

`apicli call` is self-sufficient: it establishes the session on the first call
and re-establishes it on expiry, both non-interactively. **Do NOT run `apicli
login` as a warm-up step** — go straight to `call`.

```sh
# Make calls — session is created on first use and injected automatically.
# No prior `apicli login` needed when creds are in actors.yaml.
apicli call svc-login /api/me --actor alice
apicli call svc-login /api/order --actor alice --env pre \
      -X POST -H 'Content-Type: application/json' -d '{"x":1}'

# Debug — print equivalent curl without executing
apicli call svc-login /api/x --actor alice --curl

# Check / clear
apicli whoami svc-login --actor alice
apicli logout svc-login --actor alice
```

Run `apicli login <app>` explicitly **only** when the app's auth is interactive
— it needs a per-login `--var` that can't live in `actors.yaml` (OTP,
`verifyCode`). That case surfaces as `{error: {code: "SESSION_EXPIRED"}}` (see
below).

## Auto-login + auto-relogin — the key capability

`apicli call` owns the whole session lifecycle. You never call `login` to prime
it, and never do a `whoami` → `login` dance:

1. **No session yet** → `call` runs the auth flow automatically (non-interactive
   when every `vars_required` is in `actors.yaml`/`vars_defaults`), saves it,
   then sends the request.
2. **Stored session stale** → `expired_when` matches + all creds available →
   re-login once, retry once → `{data}` with `warnings: ["session re-established"]`.
3. **Interactive auth needed** (a required `--var` is missing) or retry still
   fails → `{error: {code: "SESSION_EXPIRED"}}` → *now* run
   `apicli login <app> --var ...` explicitly, then resume calling.

So the only reason an agent ever types `apicli login` is case 3 — everything
else is handled inside `call`.

Sessions are keyed `(app, actor, env)` so multiple accounts / envs coexist
without collision.

## Output contract (parse this)

A reachable response is **always** `{data}` — a business failure is a real answer,
not an error:

```json
{"data": {"status_code": 200, "ok": false, "headers": {...},
          "body": {"code": 4001, "msg": "余额不足"}}}
```

`{error}` only for transport / config / auth faults (no useful body):

```json
{"error": {"code": "SESSION_EXPIRED", "message": "..."}}
```

`ok` is set from the app's `ok_when` predicate — it is **not** just HTTP 2xx.
Exit codes: `0` ok · `1` business-fail (ok:false, body still in data) · `2`
config · `3` auth / SESSION_EXPIRED · `4` timeout · `5` remote/transport.

The default JSON envelope is already what an agent wants — **don't pass `--output`**.
(`--output raw` returns the bare body text, and `--pretty` indents — both for humans.)

## call flags

| flag | short | default | description |
|---|---|---|---|
| `--request` | `-X` | `GET` | HTTP method |
| `--header` | `-H` | — | request header (repeatable) |
| `--data` | `-d` | — | request body (raw passthrough) |
| `--output` | — | `json` | `json` (envelope) or `raw` (body only) |
| `--output-file` | — | — | stream the response body to a file (binary-safe; envelope keeps metadata) |
| `--headers-file` | — | — | write `{status_code,url,headers}` JSON to a file |
| `--actor-file` | — | — | inline one-off actor (`{vars:{...}}`) instead of `actors.yaml` |
| `--allow-cross-origin` | — | false | permit sending the session to a host other than the app base |
| `--connect-timeout` | — | 30s (0 ⇒ falls back to 30s, not unlimited) | request timeout |
| `--curl` | — | false | print equivalent curl with injected auth, don't execute |

`login` also takes `--var k=v` (repeatable) for per-login one-time values (OTP).

Body is always raw passthrough — `-d 'a=1&b=2'` with a form `Content-Type`
reaches the server unchanged. No implicit JSON re-encoding.

## Login-flow fidelity (you CAN model complex legacy auth)

The login flow is not limited to one request + one bearer header:

- **Multi-step** — `flow` is a list; step N's request template sees both the
  actor vars and **every var captured by earlier steps** (cross-step). Use it for
  login → exchange → verify chains.
- **Per-step `assert`** — a predicate (same grammar as `ok_when`) checked after
  the step; false → login fails `AUTH_FAILED` (so a wrong password is caught at
  login, not on the next call).
- **`cookie_from_set_cookie: true`** — capture that step's `Set-Cookie` into the
  session and replay it on later steps + every call (cookie-based legacy auth).
- **Multi-header inject** — `inject.headers` is a map; each value is a template
  rendered from session vars, all injected on every call (e.g. `Client-Id` +
  `Authorization` + `Access-Token` together). `inject.header` (single) still works.
- **`vars_defaults`** — static fallback vars (e.g. `client_id`, `terminalType`).
  Precedence: `vars_defaults < actor/--var < captured`.
- **`extra_headers`** — per-app constant headers on every request (a per-call
  `-H` of the same name overrides).

This covers cookie-replay logins, multi-header APIs, and multi-step OTP flows —
do NOT assume apicli can't express a legacy auth shape; check these first.

## File downloads, inline actors, trace, cross-origin

- **`--output-file <p>` / `--headers-file <p>`** — stream a binary/export body
  (xlsx/pdf/zip) to disk instead of inlining it; the envelope carries metadata
  (`body_file`, `body_bytes`, `content_length`, `stream_complete`, `sha256`).
  Use for export endpoints — never inline binary into `data.body`.
- **`--actor-file <p>`** — a complete one-off actor (`{vars:{...}}`) inline,
  instead of an `actors.yaml` entry. On `call`/`login`/`whoami`/`logout`.
- **`--var k=v`** (login only) — a per-login one-time value that can't be static
  config (OTP / `verifyCode`). Visible in argv → sensitive-arg warning.
- **`trace_field`** → emits `data.trace_id`; on a not-OK response apicli prints a
  stderr pivot hint `→ logcli sls trace <id> --target <log_env>`.
- **Cross-origin guard** — a call whose host ≠ the app's base/env host is blocked
  (`API_CROSS_ORIGIN_URL`) unless `--allow-cross-origin`; the session never
  silently leaks to a foreign host.

## Config

```yaml
# ~/.aidev-clis/apicli.yaml
apps:
  svc-login:
    base_url: http://uat.example.com
    envs:
      pre:  http://pre.example.com
      prod: https://prod.example.com
    default_actor: alice
    extra_headers:               # constant headers on EVERY call (a per-call -H overrides)
      Client-Id: demo_app_a
    trace_field: header.X-Trace-Id   # surfaced as data.trace_id; header.<canonical-name> | body.<dotpath>
    log_env: log_uat                 # logcli env appended to the failed-call trace hint
    auth:
      kind: flow          # flow | none  (cookie replay is per-step, NOT a kind)
      vars_defaults: { orgId: "999" }   # lowest-precedence fallbacks: vars_defaults < actor/--var < captured
      vars_required: [phoneNo, password]
      flow:
        - request: |
            POST /auth/login
            Content-Type: application/json

            {"phoneNo":"{{phoneNo}}","password":"{{password}}"}
          assert: "body.code == 0"           # per-step: false → login fails AUTH_FAILED
          cookie_from_set_cookie: true        # capture this step's Set-Cookie into the session
          capture:
            token: body.data.token            # later steps' templates ALSO see captured vars (cross-step)
      inject:
        headers:                  # MULTI-header inject; each value rendered from session vars
          Authorization: "Bearer {{token}}"
          Access-Token: "{{token}}"
        # header: "Authorization: Bearer {{token}}"   # single-header shorthand still works
        # cookie: "SESSION={{token}}"                  # OR inject a templated cookie
    response:
      ok_when:      "body.code == 0"
      expired_when: "body.code == 401"

# ~/.aidev-clis/actors.yaml
actors:
  svc-login:
    # plain values work for dev; `secret:<name>` resolves from
    # ~/.aidev-clis/credentials/<name> (0600) instead of storing the secret here.
    alice: { phoneNo: "13800000000", password: "secret:svc_login_alice_pw", orgId: "999" }
    bob: { phoneNo: "13800000001", password: "secret:svc_login_bob_pw", orgId: "999" }
```

The bundled `apicli.yaml` / `actors.yaml` are reference templates — copy them to
`~/.aidev-clis/` and fill in your apps/actors (real configs never leave the
machine). You can offer to write them for the user from these samples.

Sessions live at `~/.aidev-clis/sessions/<app>/<actor>/<env>` (mode 0600).

For the shared `~/.aidev-clis` layout, the credentials model (`actors.yaml` is
shared with tcli), and from-zero setup steps, see the **use-aidev** skill.

## Don't use apicli for

- Simple token-auth APIs (a static `Authorization` header) — use raw `curl`.
- Database reads — use `dbcli`.
- Jenkins builds / deploys — use `jcli`.
- Log reads — use `logcli`.
- An assertion you want kept + re-runnable with a PASS/FAIL verdict — a post-deploy gate or a cross-system regression invariant — use `tcli`.

## Audit

Every invocation appends to `~/.aidev-clis/audit/<YYYYMMDD>.jsonl` (30-day auto-prune).
Non-GET calls write a `started` line before dispatch and a terminal line after, sharing an
`id`. Credential values are never written.
