# jcli

> **Status.** Primitives (`targets`/`build`/`status`/`log`/`cancel`), the configurable
> **deploy flow** (`deploy` + `extract` + allowlisted `steps`), and **discovery**
> (`jobs` cache/`--sync`, `params`) all work. Shell completion (`--target`,
> `--group`, `<service>`) ships with jcli. Design:
> `docs/superpowers/specs/2026-06-27-jcli-design.md`.

jcli gives an AI agent (and a human) controlled, audited access to **trigger and
watch Jenkins builds**. It speaks only the Jenkins REST API (native Go HTTP, API
token); the credential is injected by the client and never appears in the AI's
context or output. Unlike the old jcli, it does **not** bundle kubectl — deploy
actions are a configurable flow (Phase 2), not hardcoded here.

## Synopsis

```
jcli deploy <service> [--target n] [--group <name>] [--param k=v ...] [--no-wait] [--timeout d] [--job <name>]
jcli build  <service> [--target n] [--group <name>] [--param k=v ...] [--wait] [--job <name>]
jcli status <service> [--target n] [--group <name>] [--build N] [--job <name>]
jcli log    <service> [--target n] [--group <name>] [--build N] [-f] [--job <name>]
jcli cancel <service> [--target n] [--group <name>] [--build N] [--job <name>]
jcli jobs   [--target n] [--sync]
jcli params <service> [--target n] [--group <name>] [--job <name>]
jcli doctor [--target n]
jcli targets
```

`jcli doctor` is a staged health probe: `config` (target + credential) → `auth`
(reaches Jenkins and the token authenticates, via `/me/api/json`). It emits a flat
`{"data":[{"name","ok","detail"},...]}` (same shape as `targets`/`jobs`) and exits
non-zero if any stage fails — the failing stage's `detail` says why.

- **Everything is service-centric:** a `<service>` is resolved to a Jenkins job
  name via the env's templates (see Config). `--job <name>` bypasses resolution
  to target a raw job (non-deploy / off-template jobs).
- `build` triggers a build; `--wait` blocks until it finishes (non-SUCCESS →
  exit 5). `--param k=v` is repeatable.
- `status` reports the current/last build (`--build N`, default last).
- `log` prints the console; `-f` streams it live as NDJSON. `--output raw` prints
  raw console text instead of the JSON envelope (with `-f`, a raw live stream).
- `cancel` aborts a running build.

```
jcli targets                                            # which Jenkins instances exist
jcli build orders --target uat --param branch=main --wait
jcli status orders --target uat
jcli log orders --target uat -f
jcli cancel orders --target uat
jcli build nightly --job housekeeping/run               # raw job, no service template
```

## Config

```yaml
# ~/.aidev-clis/jcli.yaml
default_target: uat
targets:
  uat:
    adapter: jenkins                   # required by the target loader
    description: UAT Jenkins            # shown by `jcli targets` — what this target is for
    base_url: https://jenkins.example.com
    credential: jenkins.uat            # {"user","api_token"} JSON (0600), never seen by the AI
    # insecure_skip_verify: true       # internal Jenkins with self-signed TLS
    # ca_cert: /etc/ssl/corp-ca.pem    # OR pin a private CA
    groups:
      frontend:
        job_template: "front-end/grp/${service}"
        # job_templates: { "fe-": "front-end/special/${service}" }  # longest prefix wins
        # job_overrides: { legacy-ui: "front-end/legacy/build" }    # exact (folder path ok)
      backend:
        job_template: "app-server/basic/${service}"
```

**`groups:` (required)** — one target (Jenkins connection) hosts one or more stacks. Each
group carries its own `job_template`/`job_templates`/`job_overrides`, `deploy` flow, and
`vars`. Select with `--group <name>`; `--group` may be omitted only when the target defines
exactly one group. With multiple groups and no `--group`, jcli **auto-resolves** the group
from the service name using the synced jobs cache: for each group it computes
`ResolveJobName(service)` and checks whether that path exists in the cache — the group
whose resolved path is present wins. Exactly one match → that group is used; two or more
matches → `GROUP_AMBIGUOUS` (pass `--group`); no match → `SERVICE_UNKNOWN` (run
`jcli jobs --sync`, or pass `--group`/`--job`); cache never synced → `GROUP_REQUIRED`.
`--group` and `--job` remain explicit overrides that bypass auto-resolution entirely.

`--job <path>` bypasses service routing. For `build`/`status`/`log`/`cancel`/`params`
the group is irrelevant, so no `--group` is needed even on a multi-group env. `deploy`
does need a group (it runs the group's deploy flow), so it **infers** the group from the
job path: the group whose routing would produce that path (an override value, or a
template that reverses and round-trips through `ResolveJobName`) wins. Exactly one →
inferred; otherwise (`JOB_NO_GROUP` / `GROUP_AMBIGUOUS`) pass `--group`. Inference never
picks a wrong group — worst case it asks for `--group`.

**Service → job resolution** within the selected group (high → low): `job_overrides`
(exact) → `job_templates` (longest service-prefix, `${service}` substituted) →
`job_template` (catch-all) → the bare service name. A resolved job may be a
`/`-separated folder path (`team/build-svc` → `/job/team/job/build-svc`).

The credential file is JSON `{"user":"...","api_token":"..."}`, mode 0600. Auth is
HTTP Basic + a best-effort CSRF crumb.

## Deploy flow

`jcli deploy <service>` runs a configurable flow: **build → wait → extract values
from the console → run optional steps.** It is decoupled from kubectl — the steps
are just commands that *happen* to be `kubectl` (or `ssh`/`helm`/…). With no
`steps:`, `deploy` simply emits the extracted values.

```yaml
deploy:
  params: { branch: "${branch}" }                 # POSTed to the build (caller --param overrides)
  extract:                                          # name → regex over the console; LAST capture wins
    tag: 'registry/uat/${service}:(\d{14})'
  steps:                                            # optional; argv arrays (no shell)
    - [kubectl, -n, uat, set, image, "deploy/${service}", "${service}=registry/uat/${service}:${tag}"]
    - [kubectl, -n, uat, rollout, status, "deploy/${service}", --timeout=5m]
  # step_binaries: [kubectl, ssh, helm, docker, bash]   # override the default allowlist
  # step_env: { KUBECONFIG: "${vars.kubeconfig}" }      # env for tools that read env, not flags
vars: { kubeconfig: ~/.kube/uat.yaml }              # template values (~ expanded)
```

- **Templating** uses `${...}`: `${service}`, `${param.k}` (or bare `${k}`),
  `${vars.X}`, extract names like `${tag}`, and `${artifacts.N}` (bare index =
  fileName), `${artifacts.N.fileName}`, `${artifacts.N.relativePath}` (N is 0-based,
  in steps). A literal `{...}` passes through (so `kubectl ... -p '{"spec":…}'` and
  jsonpath `{.status}` are fine); `$$` is a literal `$` so a `bash -c` step can use
  the shell's own `${VAR}`. An unknown `${token}` is a hard error.
- **Steps are argv arrays, never a shell** (unless you explicitly write
  `[bash, -c, "…"]`), and `argv[0]` must be in the allowlist — default
  `kubectl`/`ssh`/`helm`/`docker`/`bash`, overridable per env with `step_binaries`.
  The AI supplies only `--param`/`service`; it can't author or alter a command.
- **`--timeout`** (default 15m) bounds the whole deploy; a hung step is cancelled.
- **Failure stops the flow:** a non-SUCCESS build (`BUILD_FAILED`), an extract with
  no console match (`EXTRACT_NO_MATCH`), a disallowed binary (`STEP_BINARY_DENIED`),
  or a step's non-zero exit (`STEP_FAILED`) — steps never run after a failed build.

```
jcli deploy orders --target uat --param branch=main
# → {"data":{"job":"orders-uat","build":412,"result":"SUCCESS",
#            "extracted":{"tag":"20260628..."},"steps":[{"argv":[...],"exit":0}],
#            "artifacts":[{"fileName":"orders-1.2.3.jar","relativePath":"target/orders-1.2.3.jar"}]}}
```

**`artifacts`** (in `build --wait` and `deploy` output): array of the build's archived
artifacts, each `{"fileName":"...","relativePath":"..."}`. Omitted (`omitempty`) when the
build archived nothing (e.g. image-push pipelines, or when `--wait` is not set). Read it
absence-safe: `jq -r '.data.artifacts[0].fileName // empty'`.

For the build→deploy step itself, point a step at another Jenkins job, `kubectl`,
or (later) a dedicated CLI — jcli stays a Jenkins capability broker.

## Discovery

```
jcli jobs --target uat --sync       # recursively walk Jenkins → refresh the cache
jcli jobs --target uat              # read the cache (offline, fast)
jcli params orders --target uat     # a job's static parameter defs
```

- **`jcli jobs`** reads a local cache at `~/.aidev-clis/cache/jcli/<target>/jobs.json`
  (offline). **`--sync`** refreshes it by recursively walking the Jenkins folder
  tree, collecting buildable leaf jobs. The cache stores **identity only** —
  `{name, path, url}` per job + `synced_at` — never build state (that's `status`)
  or parameters (that's `params`).
- **You still type the short service name everywhere** (`jcli deploy orders`);
  resolution is via the templates as usual. The cached full `path` is for
  completion, display, and `--job`. As a convenience, when the templates don't
  resolve a service, jcli falls back to the cache (leaf name → full path); a leaf
  that matches multiple jobs returns `JOB_AMBIGUOUS` (disambiguate with `--job`).
- **`jcli params <service>`** lists static parameter definitions (name / type /
  default / choices). Reactive/Active-Choices options are not computed.
- Shell completion ships with jcli: `--target` and `--group` complete from config,
  and the `<service>` positional completes from the jobs cache (build/deploy/
  status/log/cancel/params). It's wired via cobra, not a separate subcommand.

## Security model

**Primary (backend enforced):** the Jenkins API token's own scope — it can only
trigger/read the jobs its user is granted. That is the real boundary; the AI never
sees the token.

**Secondary (defense-in-depth):** jcli performs only the build/read/cancel/deploy
verbs above; deploy `steps` are **operator-authored** argv (the AI fills only
`${...}` params, can't author commands) and `argv[0]` is allowlisted. Every
invocation is audited to the current day-file
`~/.aidev-clis/audit/<YYYYMMDD>.jsonl` (30-day retention).
Side-effecting verbs (build/deploy/cancel) write a `started` line before
dispatch and a terminal line after, sharing an `id`, so a kill still leaves a
trace. Credentials are never written.

## Output contract

Success `{"data": <payload>}`; error `{"error":{"code","message"}}`. Exit codes
0 ok / 1 general / 2 config / 3 auth / 4 timeout / 5 remote (Jenkins/build
failures). `log -f` streams NDJSON (`{"type":"record",...}` / `{"type":"end",...}`).
See `docs/OUTPUT-CONTRACT.md`.
