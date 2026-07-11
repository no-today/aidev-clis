---
name: use-aidev
description: Use when an AI agent enters a workspace and needs to discover which aidev CLIs (dbcli/logcli/jcli/apicli) are usable here, and — when a task targets one of several configured targets — which CLIs apply to that target. `aidev` is a read-only discovery command scoped to the workspace's active scene. Also the shared-setup reference — first-time configuration of `~/.aidev-clis` (config-dir layout, the credentials model, the copy-a-template-and-fill workflow) and config backup/restore.
---

# use-aidev

`aidev` answers two questions, scoped to the current workspace's **scene**
(resolved from the nearest `.aidev.yaml`, or the `AIDEV_SCENE` env var; no
marker ⇒ everything is in scope). It is **read-only and never dispatches** —
you still call `dbcli`/`logcli`/`jcli`/`apicli` directly to do work.

Run it with no flags — output is the JSON envelope by default (AI-first); do
**not** pass `--output`:

```
aidev
```

## Output contract (parse this)

```json
{"data":{
  "workspace": {"scene":"companyA","source":"…/.aidev.yaml"},
  "tools": ["apicli","dbcli","jcli","logcli"],
  "targets": {"a-uat":{"clis":["dbcli","jcli","logcli"]}, "a-prod":{"clis":["logcli"]}},
  "apps": ["svc-login"]
}}
```

- `workspace.scene` is the active scene (`null` when unscoped).
- **`tools`** — which CLIs are usable in this workspace at all. Use on entry to
  learn your toolbox.
- **`targets`** — for each configured target, which target-keyed CLIs apply.
  Capability varies: `a-uat` is full (db+jenkins+log); `a-prod` is **log-only**.
  When a task names (or implies) a target, read its `clis` to pick the
  right tool; if the target is ambiguous, ask the user which one.
- **`apps`** — apicli is app-keyed (not target-keyed), so its in-scope apps are
  listed here as a sorted string array; each is callable via `apicli … <app>`.

A tool missing from `tools` is not configured in this scene — don't use it here.

A sample scene marker, `.aidev.yaml`, is bundled alongside this skill — drop it at
a project root to scope discovery to one scene.

## Setting up `~/.aidev-clis`

All CLIs read one shared config dir, `~/.aidev-clis/` (override with the
`AIDEV_CLIS_HOME` env var). Layout:

```
~/.aidev-clis/
  dbcli.yaml  apicli.yaml  jcli.yaml  logcli.yaml   # per-CLI: targets/apps + credential refs
  actors.yaml                                        # shared identities (apicli + tcli)
  credentials/                                       # secret files, mode 0600 — the AI never reads these
  sessions/  cache/  audit/  backups/                # runtime state (auto-created)
```

**The AI writes the `*.yaml`, the human owns `credentials/`.** That split is the
project's whole point — config files carry only a `credential:` *reference*, never
a secret, so the AI can write and edit configs without ever handling one.

To configure a CLI from zero:

1. **Copy its template.** Each `aidev-<cli>` skill ships a fully-commented
   `*.yaml` next to it (installed by `make install`). Copy it to
   `~/.aidev-clis/<cli>.yaml`. When helping a user, you can *offer to scaffold*
   this YAML from that sample — fill in targets/apps, leave secrets as
   `credential:` references.
2. **The human creates the credential file(s).** Each `credential: <name>` points
   at `~/.aidev-clis/credentials/<name>` (mode 0600). The payload shape is
   per-CLI — a bare password, a `{user,api_token}` JSON, an AK/SK JSON, a
   kubeconfig — documented in that CLI's own skill. In `actors.yaml`, an actor var
   can be `secret:<name>` to pull from `credentials/` instead of inlining it.
   The credential's *own* scope (a read-only DB account, a logs-only SA) is the
   real security boundary — provision it narrow.
3. **Verify.** `<cli> targets` (or `apicli` app calls) should list what you added;
   `aidev` should now show the CLI/target in scope.

## Config backup/restore

Discovery is read-only, but `aidev config` is the one mutating exception — it
backs up and restores your `~/.aidev-clis` setup (top-level `*.yaml` plus
`credentials/`):

```
aidev config backup                     # write <home>/backups/aidev-config-<ts>.tar.gz
aidev config restore <archive.tar.gz>   # aliases: import, use
```

`restore` writes config files, taking a safety backup of the current config
first (unless `--no-backup`).
