# Security Model

## Core principle: authorization belongs to the backend

The CLI does NOT re-implement authorization. It injects a least-privilege
credential and lets the backend enforce. This is what keeps the design KISS —
there is no command-filter treadmill to maintain, no growing list of blocked
patterns to keep airtight.

## What this does and does not defend against

Be precise about the boundary — it is easy to overread. The CLI runs as the
invoking user's OS account. A process with that account's shell can read the
credential files under `~/.aidev-clis/`, and — more to the point — it usually
has *other* paths to the same systems (an ssh key to a bastion, a kubeconfig,
cloud CLI credentials) that this suite never touches.

- **This is not a sandbox against a malicious or compromised same-user
  process.** Keeping the DB password out of the CLI's output does not stop such
  a process; the account's weakest credential path (often ssh) is the real
  exposure and it lies outside this tool.
- **What it does buy, in the trust-fully setting it is built for:** least-
  ceremony structured access; read-only defaults with an explicit write switch
  (smaller blast radius for *mistakes*); credentials kept out of the agent's
  transcript and logs (less *accidental* leakage); and a complete, replayable
  audit trail.
- **Where real control lives:** backend authorization (grants / RBAC / token
  scope — the primary layer below); host-side per-action approval for
  higher-risk operations; and, for genuinely sensitive data, organizational
  governance (vetted models, data residency) plus backend-enforced masking or
  read replicas. In-CLI isolation is necessary-but-not-sufficient, and on a
  full-access machine it mostly moves *accident* and *visibility* — not the
  trust boundary itself.

## Two-layer model

| Layer | Mechanism | Who enforces |
|---|---|---|
| **Primary** | A scoped credential the operator provisions (read-only DB user / logs-only k8s ServiceAccount / job-scoped Jenkins token / `log:GetLogStoreLogs` RAM policy). The CLI injects it; the AI never sees it. | the backend (RBAC / grants / token scope / RAM) |
| **Secondary (defense-in-depth)** | A small, hardcoded per-adapter allowlist of the verbs/statement-types that adapter is meant to permit (e.g. kubectl → `logs`; SQL → `SELECT`/`SHOW`/`EXPLAIN`). A loose sanity net, NOT the sole boundary. | the adapter |

The primary boundary is the credential's own scope. Because that layer already
does the real work, the secondary allowlist can stay small and does not need to
be airtight. It is a defense-in-depth check, not the fence.

## Operator responsibility

The credential you give a CLI must be scoped to exactly what you want the AI
to do. If you give `dbcli` a superuser account, the primary layer is gone and
the secondary allowlist is the only remaining check — which is not a sound
posture. Scope the credential first; everything else follows.

Backends without fine-grained authorization (docker, whose Unix socket is
effectively root) have no strong primary layer, so they lean harder on the
secondary allowlist. This is a known limitation, not an oversight.

The SSH backends (`ssh-file`, `ssh-docker`) are the same case: a normal SSH
login grants a full remote shell, so the SSH key is *not* a scoped primary
boundary. The CLI shell-quotes every pass-through token so an agent cannot
inject remote commands through a container name or `--tail` value, but the
quoting is the fence only as far as the fixed `tail` / `docker logs` command
reaches. For real least-privilege over SSH, scope the key itself — an
authorized_keys `command="..."` forced command or a restricted shell — so the
remote side, not the CLI, enforces the boundary.

dbcli's optional `ssh:` tunnel (key / encrypted-key / password) is a transport,
not an authorization layer: it only moves the DB connection through a bastion —
the database's own read-only account is still the boundary. Host-key
verification is not pinned in v1 (`InsecureIgnoreHostKey`), so the bastion
network path must be trusted; pinning is a future addition.

## Invariants

These hold for every CLI, every adapter, every invocation:

1. **Credentials stay out of the CLI's output and audit.** (This bounds
   *accidental* exposure into the agent's transcript/logs — not a guarantee
   against a determined same-user process; see the boundary section above.)
   The credential is
   read from `~/.aidev-clis/credentials/<name>` (0600 enforced) and injected by the
   adapter directly into the subprocess environment or request — never echoed,
   never returned in the JSON envelope, never logged verbatim.
2. **Audit payloads are not redacted.** The audit record stores the full command,
   outcome, and — where applicable — a request and result summary. It discards
   response bodies, but commands and requests may still contain sensitive
   business data. Credential bytes stay absent because adapters inject them
   separately, not because the audit layer filters secrets. Audit day-files are
   created with `0600` permissions.
3. **Every invocation is audited.** A JSON line is appended to the current
   day-file `~/.aidev-clis/audit/<YYYYMMDD>.jsonl` on every run: `time`, `tool`,
   `backend`, `target`, the full-argv `command`, `outcome`, and — when they apply
   — an error `code`, a `request` summary (apicli/jcli), and a `result` summary.
   Side-effecting ops audit in two phases (a started line, then a terminal line)
   that share an `id`, so a killed run still leaves a trace. There is no silent
   path. Day-files older than 30 days are pruned automatically.
