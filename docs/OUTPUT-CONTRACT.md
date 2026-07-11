# Output Contract

## Input is native; output is structured

Input to every CLI is pass-through — raw SQL, raw `kubectl logs` arguments,
raw log search parameters. Nothing new for the AI to learn; no DSL.

Output is a minimal, consistent JSON envelope. Every CLI uses the same shape.
The AI reads `obj.data` on success and `obj.error` on failure, with no other
top-level fields to track.

## Envelope shape

**Success**

```json
{"data": <payload>}
```

The `"warnings"` field is added only when there is something the AI must act
on or know about — for example, when an automatic LIMIT capped the result set:

```json
{"data": [...], "warnings": ["auto-LIMIT truncated results to 100 rows"]}
```

`"warnings"` is omitted entirely when there is nothing to report.

**Error**

```json
{"error": {"code": "AUTH_FAILED", "message": "credential rejected by remote"}}
```

Errors are written to stdout, not stderr. This is intentional: an AI agent
that reads only stdout still sees the error. The exit code co-signals the
failure class (see below), but the JSON is the authoritative machine-readable
form.

**Streaming** (`-f`, tail)

NDJSON: one JSON object per line, flushed immediately after each record.

```
{"type": "record", "data": {"ts": "2026-06-26T10:00:01Z", "msg": "started"}}
{"type": "record", "data": {"ts": "2026-06-26T10:00:02Z", "msg": "processing"}}
{"type": "end", "count": 2}
```

On stream error the terminal line is:

```
{"type": "error", "error": {"code": "TIMEOUT", "message": "read deadline exceeded"}}
```

## `doctor` — staged health checks (dbcli, jcli, logcli)

The capability CLIs that reach a remote backend (`dbcli`, `jcli`, `logcli`)
expose `doctor`; `aidev`/`apicli`/`tcli` do not. Where present, `doctor` shares
one standard payload shape: `data` is a **flat, ordered list of stage checks**,
identical across those CLIs.

```json
{"data": [
  {"name": "config",  "ok": true,  "detail": "adapter mysql"},
  {"name": "ssh",     "ok": true,  "detail": "tunnel ok"},
  {"name": "connect", "ok": false, "detail": "DB_PING: connection refused"}
]}
```

- Each stage is `{name, ok, detail}` and they run in order. The stage set is
  per-CLI — e.g. dbcli `config → [ssh] → connect`, jcli `config → auth`.
- **Overall health is the exit code**, not a top-level field (no redundant
  `ok`): `0` when every stage is ok, otherwise the failing stage's error class.
- A failed stage is reported **inside `data`** (with a non-zero exit), so the
  caller always gets the full staged diagnosis. This is the one deliberate
  exception to "non-zero exit ⇒ `error`" below. (A failure to even resolve the
  env/credential — before any stage runs — is still a normal `{error}`.)

## What is NOT in the envelope

| Field | Why absent |
|---|---|
| `status` | Redundant with the exit code. Every response is either `data` or `error`; a `status` field adds nothing. |
| `meta` nesting | Adds indirection with no benefit. |
| `count` at top level | Redundant with `len(data)` for array payloads. |
| `elapsed_ms` at top level | Operational noise; not useful to an AI acting on the data. |

The top level contains exactly one of `data` or `error`, optionally
accompanied by `warnings` and — when `-v`/`-vv` is set — `diagnostics`.
Nothing else.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success |
| `1` | General error |
| `2` | Configuration error (missing env, bad YAML, unknown adapter) |
| `3` | Authentication error (credential rejected, session expired) |
| `4` | Timeout |
| `5` | Remote error (backend returned an error or was unreachable) |

Exit code and JSON envelope always agree: exit 0 implies `data` present; exit
non-zero implies `error` present — **except `doctor`, which reports a failed
stage inside `data` with a non-zero exit (see above).**
