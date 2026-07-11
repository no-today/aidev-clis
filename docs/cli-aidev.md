# aidev — workspace-scoped discovery

`aidev` is a **read-only discovery** command. It never dispatches to the other
CLIs — you still call `dbcli`/`logcli`/`jcli`/`apicli` directly. It answers, for
the current workspace: which CLIs are usable, and per environment which CLIs
apply. Output is the AI-first `{data}` JSON envelope by default; pass
`--output raw` for a human summary, `--pretty` to indent.

Discovery is read-only, but `aidev` also carries one mutating subcommand,
`config`, for backing up and restoring your `~/.aidev-clis` setup (top-level
`*.yaml` files plus `credentials/`, bundled as a `.tar.gz`):

```sh
aidev config backup                       # write <home>/backups/aidev-config-<ts>.tar.gz
aidev config restore <archive.tar.gz>     # aliases: import, use
aidev config restore --no-backup <archive.tar.gz>   # flag precedes the positional
```

`restore` takes a safety backup of the current config first (unless
`--no-backup`) before writing the archive's files. `config` is the one mutating
exception; everything else `aidev` does is read-only.

## Scenes

A **scene** scopes discovery to one workspace. Resolution precedence:

1. `AIDEV_SCENE` env var (escape hatch for CI/testing)
2. the nearest `.aidev.yaml` walking up from the cwd (`scene: <name>`)
3. none — **no marker ⇒ no scoping**, every env/app is visible (today's behavior)

Tag a target with `scene: <name>` to group it: on a **target** block for
`dbcli`/`jcli`/`logcli`, or on an **app** block for `apicli`. A target untagged
**across all tools** is **global** — in scope for every scene. (Target
scope is the *union* of its `scene:` tags across the target-keyed tools, so tag a
shared target name consistently in every tool, or one tagged entry scopes the whole
target.) Single-target setups need no tags and no marker.

## Output

```json
{"data":{
  "workspace": {"scene":"companyA","source":"…/.aidev.yaml"},
  "tools": ["apicli","dbcli","jcli","logcli"],
  "targets": {"a-uat":{"clis":["dbcli","jcli","logcli"]}, "a-prod":{"clis":["logcli"]}},
  "apps": ["svc-login"]
}}
```

- `workspace.scene` — the active scene (`null` when unscoped); `source` is where
  it came from.
- `tools` — CLIs usable in this workspace (first-class entry view).
- `targets` — target→capability matrix across the target-keyed CLIs. Capability
  varies (a full target vs. a log-only one). The **target identity is the target
  name**, joined across `dbcli.yaml`/`jcli.yaml`/`logcli.yaml`.
- `apps` — apicli is **app-keyed**, not target-keyed (its nested `envs:` are
  per-app base-URL variants), so its in-scope apps are listed here as a plain
  sorted array of app names. Apps are always served by apicli, so the per-app
  `clis` wrapper was dropped; each app is callable via `apicli … <app>`.
- `notes` — per-tool problems (e.g. an unparseable config); a tool whose config
  is simply absent is skipped silently.

## Out of scope (by design)

`aidev` only *reports* scope; the four CLIs do **not** enforce it — a `--target`
outside the active scene is not refused. Hard per-CLI enforcement is a possible
later phase. `aidev` also does no network reachability probing: it reports what
is *configured and in scope*, not what is live.
