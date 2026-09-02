# apicli `-F/--form`: binary-safe multipart upload

**Status:** implemented on branch `form-upload`
**Date:** 2026-09-02

## Problem

`apicli call` has exactly one request-body entry point: `-d/--data`, a raw
string passthrough (`Body: []byte(data)` in `cmd/apicli/commands.go`). There is
no path from a file on disk into the request body. That leaves the suite with a
half-covered contract: `--output-file` makes *downloads* binary-safe (streamed
to disk, never inlined into the JSON envelope), but there is no equivalent for
the *upload* direction.

Attempting multipart through `-d` fails for two independent reasons, both
observed in practice against a Spring `List<MultipartFile>` endpoint:

1. **Trailing-newline loss.** Bash command substitution `$(...)` strips *all*
   trailing newlines. A multipart body's closing delimiter is byte-sensitive
   (`\r\n--boundary--\r\n`), so the string reaching `-d` no longer matches the
   byte count verified on the source file. Tomcat reports
   `FileUploadException: Stream closed`.
2. **NUL truncation.** Shell variables are C-string-semantic. Real PNG/JPEG
   content contains `\0` bytes, which truncate the value. This is not a
   fixable assembly mistake — the "read a file into a shell variable, pass it
   to `-d`" path is *categorically* unsound for binary.

Setting `-H 'content-type: multipart/form-data'` without a boundary produces a
correct server-side rejection (`no multipart boundary was found`), confirming
the request arrives; the defect is entirely in body construction.

## Goal

Give `apicli call` a curl-shaped `-F/--form` flag that reads file bytes
directly from disk and computes the boundary, `Content-Type`, and
`Content-Length` itself — removing the shell variable from the path entirely.

## Non-goals

Recorded as deliberate choices, not omissions:

- **`--data-binary @file`** (raw non-multipart body from a file). A real second
  shape of "upload-side binary safety", but no current need. Add when one
  appears.
- **tcli surface.** tcli's `api` step is a raw-HTTP text block; multipart does
  not fit that shape and would need a new `form:` schema key. Schema is a
  durable contract, and upload cases depend on external fixture files, which
  make poor regression cases. Not now.
- **`name=<file`** (file content as a plain field *value*, no filename). No need.
- **`@-` / stdin.** stdin is consumed once, which conflicts directly with the
  replay requirement below.
- **`--form-string`** (plain field whose value starts with a literal `@`).
  Add a flag when someone hits it.

## Constraint that shapes the design: the body must be replayable

`Call` (`internal/apicli/call.go`) sends the request, and on a stale stored
session re-logs-in and calls `DoRequest` a **second time** with the same
`*CallRequest`. Any body representation that is consumed by the first send
breaks auto-relogin. This rules out a naive streaming reader and is the reason
stdin is a non-goal.

## Flag surface

`-h` is the truth source for flags. The durable model:

```
-F, --form <part>       multipart form field, repeatable
    --max-upload <sz>   cap on total upload bytes (default 512MB)
```

| Form | Meaning |
|---|---|
| `-F 'entranceExitId=11085'` | plain field |
| `-F 'images=@/path/a.png'` | file field; filename is `filepath.Base`, Content-Type inferred from extension via `mime.TypeByExtension`, falling back to `application/octet-stream` |
| `-F 'images=@/path/a.png;type=image/png'` | explicit content type |
| `-F 'images=@/path/a.png;filename=x.png'` | override the transmitted filename |
| `-F 'images=@a.png' -F 'images=@b.png'` | repeated name → multiple parts, **order preserved**; binds to Spring `List<MultipartFile>` |

**`-F` implies POST.** When `-F` is given and `-X` was not explicitly set,
the method becomes `POST`, matching curl. Without this, the `-X` default of
`GET` would silently send a body-bearing GET. Detected via
`cmd.Flags().Changed("request")`, so an explicit `-X PUT` still wins.

**Deliberate deviation from curl:** `;type=` and `;filename=` are parsed
**only** on the `@` form. For a plain field, everything after the first `=` is
the literal value, semicolons included. curl parses `;type=` on plain fields
too; we trade that rarity for the guarantee that a field value containing `;`
is never silently truncated.

## Structure

New file `internal/apicli/form.go`, two pure functions with one job each:

```go
// ParseFormArgs validates and splits raw -F arguments. No disk I/O.
func ParseFormArgs(args []string) ([]FormPart, error)

// EncodeForm stats, reads, and encodes the parts into a complete body,
// recording each file part's size back into parts for the audit record.
func EncodeForm(parts []FormPart, maxBytes int64) (body []byte, contentType string, err error)
```

`FormPart` carries the parsed field plus, for file parts, the size `EncodeForm`
learned while stat-ing: `{Name, Value, File, Filename, ContentType, Bytes}`.
One representation serves the encoder, `ToCurl`, and the audit record — none of
them needs to re-parse or re-stat.

`CallRequest` gains two fields:

- `Form []FormPart` — the parsed parts, used *only* by `ToCurl` and the audit
  record once encoding is done.
- `ContentType string` — the `multipart/form-data; boundary=...` value computed
  by `EncodeForm`.

**Encoding happens once, in the command layer**, and its result is written into
`req.Body`. `DoRequest` changes by one line: after the per-call `-H` loop,
`if req.ContentType != "" { hreq.Header.Set("Content-Type", req.ContentType) }`.
The relogin retry therefore replays the identical `[]byte` — no disk re-read,
no boundary change, exact `Content-Length`.

Placing the `Content-Type` set *after* the `-H` loop matters: per-call `-H` is
applied last precisely so it can override injected session headers, so a
generated header placed earlier would be silently clobbered — reproducing the
original missing-boundary failure. Conflicting `-H` is rejected outright (below)
rather than relying on ordering alone.

### Size cap

The cap is a **RAM guardrail, not a contract**. Unlike `maxResponseBytes`
(which exists because response bodies are inlined into the JSON envelope),
upload bytes never enter the envelope — they go straight to the socket. The
number has no principled basis; it only stops a slip from exhausting memory.

Default `512 << 20`, overridable by `--max-upload`. The value accepts an
optional `KB`/`MB`/`GB` suffix; a bare number is bytes. The repo has no
existing size-parsing convention, so this is a small local parser with its own
table test.

Enforcement stats every file and sums the sizes **before reading any bytes**,
so an over-cap request fails without allocating.

## Errors

| Condition | Code |
|---|---|
| `-F` and `-d` both given | `errs.Config("REQUEST_INVALID", ...)` |
| `-F` and an explicit `-H 'Content-Type: ...'` both given | `errs.Config("REQUEST_INVALID", ...)` — message states that `-F` computes Content-Type and boundary itself |
| `-F` argument has no `=`, or an unrecognized modifier | `errs.Config("FORM_ARG_INVALID", ...)` |
| file missing, unreadable, or a directory | `errs.General("FORM_FILE_UNREADABLE", ...)` |
| total size over the cap | `errs.General("FORM_TOO_LARGE", ...)` — message names `--max-upload` |
| `--max-upload` not a positive size | `errs.Config("MAX_UPLOAD_INVALID", ...)` |
| multipart writer failure (unreachable in practice — the sink is a `bytes.Buffer`) | `errs.General("FORM_ENCODE_FAILED", ...)` |

The Content-Type conflict is rejected rather than ignored or honored: it
converts the exact failure that motivated this work from a server-side
`no multipart boundary was found` into a local error that says why.

## `--curl` and audit

- `--curl` renders the `-F` arguments verbatim after the headers, and does not
  render a boundary (curl generates its own). The printed command stays a
  genuinely runnable equivalent.
- The audit `req` record gains `form`: file parts as `{name, file, bytes}`,
  plain parts as `{name}` only. **No field values, no file content** — within
  that structured `request` record. This matches the existing treatment of
  `-d` (request bodies are not audited) and keeps that structured record free
  of PII.

  This guarantee is scoped to `request`/`request.form`, not the audit line as
  a whole. Every audit line also carries `command`, set from
  `audit.CommandLine(os.Args)` — the full process argv, unredacted. A `-F
  idCardNo=110101199001011234` therefore lands verbatim in `command`, exactly
  as a `-d '{"idCardNo":...}'` body already does; this is pre-existing,
  deliberate audit behavior (`internal/core/audit/audit.go`: "Payloads are NOT
  redacted — the log is 0600"), not a regression introduced by `-F`. An agent
  that cares about a particular `-F` value not landing in the audit log's
  `command` field should pass it as a file (`-F 'name=@path'`) instead of an
  inline field value.

The response envelope and output contract are unchanged.

## Testing

- `ParseFormArgs` table test: every supported form plus every error branch.
- `EncodeForm`: decode the result with `mime/multipart` and assert field
  values, filenames, Content-Types, part **order**, and repeated-name handling.
- Integration: `httptest` server calling `ParseMultipartForm`, asserting the
  Spring-style same-name multi-file binding.
- **Binary-safety regression:** a fixture containing `\x00` and `\r\n` bytes;
  assert the server-received SHA-256 equals the source file's. This pins both
  original failure modes (trailing-newline loss, NUL truncation) as tests.
- `--max-upload` size parser table test; over-cap rejection asserted to happen
  without reading file contents.
- `--curl` rendering, and both conflict branches (`-F`+`-d`, `-F`+`-H
  Content-Type`).
- Cross-platform: filenames via `filepath.Base`; fixtures generated into
  `t.TempDir()`, never committed. See [CROSS-PLATFORM.md](../../CROSS-PLATFORM.md).

## Docs

`-h` remains the truth source for flags. `docs/cli-apicli.md` and
`skills/aidev-apicli/SKILL.md` each get a short section presenting `-F`
(upload) and `--output-file` (download) as the two directions of binary safety,
with one copyable multi-file example.

## Future work

If total upload size reaches the GB range, buffering becomes the wrong choice
and the body should move to `io.Pipe` streaming with constant memory and no
cap. Replay stays achievable — a multipart body is a deterministic function of
a file path list, so relogin re-opens the files and re-encodes (with the caveat
that a file mutated mid-flight yields a different second request). The cost is
that `CallRequest.Body` becomes a reopenable factory rather than `[]byte`, and
`ToCurl` plus the audit layer follow. Deferred until a real payload demands it.
