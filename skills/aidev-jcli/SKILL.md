---
name: aidev-jcli
description: Use when an AI agent needs to trigger or watch a Jenkins build, deploy a service, check build status, tail a build log, cancel a build, or discover jobs/parameters — across configured Jenkins instances. Covers the service-centric model, the configurable deploy flow, the JSON envelope, and target-based discovery.
---

# jcli

`jcli` gives an AI agent controlled, audited access to **trigger and watch Jenkins
builds, and deploy**. It speaks only the Jenkins REST API; the CLI injects an API
token you never see and returns a minimal JSON envelope. It is **decoupled from
kubectl** — the post-build action is an operator-configured flow, not hardcoded.

```
jcli deploy <service> [--target n] [--group g] [--param k=v ...] [--no-wait] [--timeout d] [--job <name>]
jcli build  <service> [--target n] [--group g] [--param k=v ...] [--wait] [--job <name>]
jcli status <service> [--target n] [--group g] [--build N] [--job <name>]
jcli log    <service> [--target n] [--group g] [--build N] [-f] [--output json|raw] [--job <name>]
jcli cancel <service> [--target n] [--group g] [--build N] [--job <name>]
jcli jobs   [--target n] [--sync]       # discovery: list jobs (local cache; --sync refreshes)
jcli params <service> [--target n] [--group g] [--job <name>]   # discovery: a job's parameter defs
jcli doctor [--target n]                # connectivity + token auth
jcli targets                            # configured Jenkins instances
```

`--group` selects which **group (stack)** to resolve the service within; omit it
when the env has one group (or the service resolves unambiguously across groups).
`log` returns the JSON envelope by default, which is what an agent wants — **don't
pass `--output`**. `log --output raw` prints raw console text (for humans).

## Everything is service-centric (you type the short name)

You always pass a short `<service>` (e.g. `orders`); jcli resolves it to the real
Jenkins job via the env's templates (`job_overrides` exact > `job_templates`
longest-prefix > `job_template` catch-all > bare name — folder paths supported).
**Never type the full `team/job/...` path.** `--job <name>` is an escape hatch to
target a raw job that doesn't fit the templates.

Each target has one or more **groups** (stacks), and resolution happens *within* a
group. With one group it's automatic. With several, jcli auto-resolves the group
from the jobs cache when the service is unambiguous; if it can't, you get
`GROUP_REQUIRED` / `GROUP_AMBIGUOUS` — pass `--group <name>` (list them via the
config or `jcli targets`). A `--job` raw path also pins the group via its folder.

## Output contract (parse this)

- Finite verbs → one envelope: `{"data": <payload>}` or `{"error":{"code","message"}}`.
- `log -f` → **NDJSON**, one object per line: `{"type":"record","data":{"message":...}}`
  … then `{"type":"end","count":N}`. `log -f` runs until killed (no `--timeout` flag) — stop it yourself.
- `doctor` → a flat **staged-check array** `{"data":[{"name","ok","detail"},...]}`
  (`config` → `auth`); exit non-zero if a stage fails — the one case where a
  non-zero exit carries `data`, not `error`. (See docs/OUTPUT-CONTRACT.md.)
- Exit codes: 0 ok / 1 general / 2 config / 3 auth / 4 timeout / 5 remote.

## Discover before you act (you can't guess names)

```
jcli doctor --target uat            # is Jenkins reachable + my token valid?
jcli jobs --target uat              # what services/jobs exist (reads the local cache)
jcli jobs --target uat --sync       # refresh the cache from Jenkins (recursive folder walk)
jcli params orders --target uat     # what params does this job take (name/type/default/choices)
```

`jobs` reads `~/.aidev-clis/cache/jcli/<target>/jobs.json` (offline, fast); `--sync`
re-walks Jenkins. Reactive/Active-Choices params are NOT resolved (static only).

## Build vs deploy — pick deliberately

- **`build <service>`** triggers the resolved job and (with `--wait`) blocks until
  it finishes (non-SUCCESS → exit 5). No post-build steps. Use for a plain build.
- **`deploy <service>`** runs the configured flow: **build → wait → extract values
  from the console → run the operator's `steps`** (e.g. `kubectl set image`).
  **This deploys for real** — confirm intent before running it. With no `steps:`
  configured it just emits the extracted values.
- You supply only `--param k=v` (and the service). **You cannot author or change
  the deploy commands** — `steps` are operator config, `argv[0]` is allowlisted
  (`kubectl`/`ssh`/`helm`/`docker`/`bash`). Tokens are `${...}` (a build branch is
  `--param branch=x` → `${branch}`).
- `--no-wait` triggers and returns the build number without waiting.

```
jcli build  orders --target uat --param branch=main --wait
jcli deploy orders --target uat --param branch=main         # build → extract → steps
jcli status orders --target uat                              # last build result
jcli log    orders --target uat -f                          # stream the console
jcli cancel orders --target uat                              # abort a running build
```

## Infer the target first

`jcli targets` is a safe local read (name + description, no backend call). Prefer an
explicit user target or the release/UAT context; if several fit or the user is vague,
confirm once before triggering a build.

## Config (see docs/cli-jcli.md for the full schema)

```yaml
# ~/.aidev-clis/jcli.yaml
targets:
  uat:
    adapter: jenkins
    base_url: https://jenkins.example.com
    credential: jenkins.uat            # {"user","api_token"} JSON (0600), never seen by the AI
    # insecure_skip_verify: true       # internal Jenkins with self-signed TLS
    groups:                            # REQUIRED: ≥1 group (stack). --group picks one.
      backend:                         # resolution + deploy live INSIDE a group
        job_template: "${service}-uat" # service → job; or job_templates (prefix) / job_overrides (exact)
        deploy:                        # optional flow
          extract: { tag: 'registry/uat/${service}:(\d{14})' }
          steps:
            - [kubectl, -n, uat, set, image, "deploy/${service}", "${service}=registry/uat/${service}:${tag}"]
```

`job_template` / `job_templates` / `job_overrides` / `deploy` / `vars` are all
**inside a group** — an env with no `groups:` fails to load (`CONFIG_INVALID`).

The credential's own Jenkins scope (which jobs the token may trigger) is the real
boundary; jcli keeps the token out of its output and audit.

The bundled `jcli.yaml` is a reference template — copy it to `~/.aidev-clis/jcli.yaml`
and fill in your targets/credentials (real configs never leave the machine). You
can offer to write it for the user from this sample.

For the shared `~/.aidev-clis` layout, the credentials model, and from-zero setup
steps, see the **use-aidev** skill.

## Don't use jcli for

- Reading logs from running pods — use `logcli kubectl`. `jcli log` is the **Jenkins
  build console**, not pod logs.
- Arbitrary cluster ops — jcli only does build/read/cancel/deploy; deploy `steps`
  are operator-fixed, not agent-authored.

## Audit

Every invocation (including rejected ones) appends to `~/.aidev-clis/audit/<YYYYMMDD>.jsonl`
(30-day auto-prune). Side-effecting verbs (build/deploy/cancel) write a `started` line
before dispatch and a terminal line after, sharing an `id`. Credentials are never written.
