# logcli

logcli gives an AI agent controlled, audited read access to log sources.
Credentials are injected by the adapter; they never appear in the AI's context
or in the JSON output.

## Synopsis

```
logcli <adapter> [--target <name>] [--timeout <dur>] [--config-dir <path>] [--pretty] <args...>
logcli targets                               # discovery: list configured targets
```

- `<adapter>` — which log backend to talk to (see table below).
- `--target <name>` — which configured instance to use (credential + endpoint).
- `<args...>` — passed through to the adapter (verb + flags).

## Discovering targets

`logcli targets` lists every configured target so an agent can see what's available
before picking one — no `--target`, no backend call, no credential:

```
logcli targets
{"data":[{"name":"base_uat","adapter":"sls","description":"基线预发环境"},
         {"name":"app_uat","adapter":"sls","description":"示例应用预发"}, ...]}
```

The optional `description` field in each target block is purely a human/agent label
— surfaced here, ignored by the adapters.

## Discovering what to query (`ls`)

Every adapter answers a uniform two-step drilldown, the logcli analog of dbcli's
`targets` → `databases` → `tables`:

```
logcli targets                      # 1. which configured instances exist
logcli <adapter> --target x ls      # 2. which sources that target can read
logcli <adapter> --target x logs …  # 3. read one
```

`ls` always returns a `{"data":[{"name":...}, ...]}` batch of the queryable
sources for that target — dynamic for backends with many sources (kubectl
deployments, docker containers), or the configured allow-list for the static ones
(the `files` of local-file / ssh-file). An agent can thus discover everything it
may read without guessing a name. (`sls` has no `ls`: its single logstore is
fixed in config; use `doctor` to verify that config is reachable.)

## Adapters

| adapter | status | verbs |
|---|---|---|
| `local-file` | **works** | `logs` (`--tail N`, `-f`) over configured files + `ls` + `doctor` |
| `sls` | **works** | `search`/`trace` (batch), `tail` (stream), `doctor` (config check) — signed GetLogs |
| `kubectl` | **works** | `logs` (pass-through `kubectl logs`) + `ls` (`get deployments`) + `doctor` |
| `docker` | **works** | `logs` (pass-through `docker logs`) + `ls` (`docker ps`) + `doctor` |
| `ssh-file` | **works** | `logs` (`--tail N`, `-f`) over SSH, configured files only, + `ls` + `doctor` |
| `ssh-docker` | **works** | `logs` (`docker logs` over SSH) + `ls` (`docker ps`) + `doctor` |

Every adapter has a **`doctor`** verb — a staged health probe (`config` → `connect`)
returning a flat `{"data":[{"name","ok","detail"},...]}` (same shape as dbcli/jcli;
exit non-zero if a stage fails). The `connect` probe is a real, harmless command:
`kubectl version`, `docker version`, `ssh … true`, `sls` a tiny `GetLogs`, and
local-file stats the configured files. Use it to verify a target before relying on it.

## `local-file` adapter

Tails the **configured** local files — an allow-list (like `ssh-file`), so the
user can't point it at an arbitrary path. Same verb/flag surface as `ssh-file`:
`ls` lists the files; `logs` tails the last `--tail N` lines (default 200), and
`logs -f` follows for appended lines. Tail and follow are **Go-native** (no
shell-out to `tail`), so the adapter works on Windows too.

### Config

```yaml
# ~/.aidev-clis/logcli.yaml
default_target: app
targets:
  app:
    adapter: local-file
    files: [/var/log/app.log, /var/log/app.err.log]
```

### Commands

```
logcli local-file --target app ls                  # the configured files
logcli local-file --target app logs --tail 100     # last 100 lines (batch)
logcli local-file --target app logs -f             # follow → streams NDJSON
```

With more than one file, `logs` prefixes each file's lines with a
`==> <path> <==` header line (like `tail`). A finite `logs` returns a `{data}`
batch; `-f` streams NDJSON, each line flushed immediately:

```
{"type":"record","data":{"message":"2026-06-26T10:00:01Z INFO server started"}}
{"type":"end","count":1}
```

On error the response carries the code, e.g. `LOCAL_FILE_NO_FILES` (no `files`
configured) or `LOCAL_FILE_OPEN` (a path can't be read).

## `kubectl` / `docker` — pass-through adapters

Both forward the backend's native command (allowlist-filtered: only `logs`/`ls`;
auth/context-override flags are denied) and inject the credential the AI never
sees. The CLI holds the credential; the backend's own authz (k8s RBAC / docker
daemon) is the primary boundary — **give it a logs-only kubeconfig.**

```yaml
# ~/.aidev-clis/logcli.yaml
targets:
  uat_k8s:
    adapter: kubectl
    kubeconfig_credential: k8s.uat   # credential NAME → ~/.aidev-clis/credentials/k8s.uat (contents are a kubeconfig; suffix irrelevant)
    namespace: uat                   # pinned; user --namespace can't escape it
  prod_docker:
    adapter: docker
    docker_host: ""                  # empty = local socket; or tcp://host:2376
```

```
logcli kubectl --target uat_k8s ls                         # what apps can I tail?
logcli kubectl --target uat_k8s logs -l app=pbs-ca --tail 200
logcli kubectl --target uat_k8s logs -f -l app=pbs-ca      # -f streams NDJSON
logcli docker  --target prod_docker ls                     # which containers?
logcli docker  --target prod_docker logs -f my-container
```

`ls` returns the queryable targets (`{data:[{name, app}]}` for kubectl,
`{data:[{name, image, status}]}` for docker); finite `logs` returns a `{data}`
batch, `-f`/`--follow` streams NDJSON.

## `ssh-file` / `ssh-docker` — over-SSH adapters

Both shell out over the system `ssh` (key-only, `BatchMode=yes` — no password
prompts). `ssh-file` tails ONLY the configured `files` (the user can't pass an
arbitrary path); `ssh-docker` allows only `logs`/`ls` and denies daemon-override
flags (`--host`/`-H`/...).

```yaml
# ~/.aidev-clis/logcli.yaml
targets:
  app_ssh:
    adapter: ssh-file
    host: vps.example.com
    user: deploy
    identity_file: ~/.ssh/id_ed25519             # key-only auth (BatchMode)
    files: [/srv/app/app.log, /srv/app/err.log]  # the readable-file allow-list
  app_ssh_docker:
    adapter: ssh-docker
    host: vps.example.com
    user: deploy
    identity_file: ~/.ssh/id_ed25519
```

```
logcli ssh-file   --target app_ssh ls                 # the configured files
logcli ssh-file   --target app_ssh logs -f --tail 200
logcli ssh-docker --target app_ssh_docker ls          # containers on the host
logcli ssh-docker --target app_ssh_docker logs -f my-container
```

## `sls` — signed SLS OpenAPI

Reads Aliyun SLS through the official GetLogs API, signed with a static AK/SK
(signature v1). No browser, no gateway, no token refresh. The RAM policy on the
AK is the security boundary — grant only `log:GetLogStoreLogs` (every verb,
including `doctor`, uses just this one permission).

```yaml
# ~/.aidev-clis/logcli.yaml
targets:
  app_prod:
    adapter: sls
    description: prod app-service logs       # shown by `logcli targets` — what this target is for
    project: app-prod-project
    logstore: app-service-log
    endpoint: cn-hangzhou.log.aliyuncs.com   # bare region host, NOT a console URL
    credential: sls.ak       # ~/.aidev-clis/credentials/sls.ak (0600) — AK/SK JSON
    trace_field: traceId     # field `trace` matches on (default traceId)
```

```
logcli sls --target app_prod doctor                              # verify config + reachability
logcli sls --target app_prod search "level: ERROR" --from 1h --to now --size 100
logcli sls --target app_prod --from 1h --to now search "level: ERROR" --size 100
logcli sls --target app_prod trace  abc123 --from 24h --to now
logcli sls --target app_prod tail   "*" --interval 5s            # streams NDJSON
```

In an SLS field query the space around the colon is optional — `level: ERROR` and
`level:ERROR` behave identically. What matters is that the field is indexed as
key-value: querying an unindexed field returns a hard `ParameterInvalid` error, not
wrong rows. Quote values that contain spaces or special characters:
`msg: "connection failed"`.

`search` and `trace` support an explicit SLS time range via `--from` and `--to`.
Those SLS modifier flags may appear either after the verb or before it. Values
accept `now`, relative durations (`30s`, `5m`, `2h`, `1d`), Unix seconds, or
RFC3339 timestamps.

The credential file is AK/SK JSON:
`{"access_key_id":"...","access_key_secret":"...","security_token":""}`
(`security_token` empty = long-term RAM AK; set it for STS temporary creds).
`search`/`trace` return a `{data:[...]}` batch; `tail` streams NDJSON.

## Target resolution precedence

`--target` flag > `AIDEV_TARGET` environment variable > adapter default.

Resolution is **adapter-aware**: with neither `--target` nor `AIDEV_TARGET` set,
if the chosen adapter has exactly one configured target it is used by default;
otherwise `--target <name>` is required (or `default_target`, when it names a
target of that adapter). See `internal/core/config/config.go`.

## Output contract

All non-streaming responses use the shared envelope:

- success: `{"data": <payload>}`
- error: `{"error": {"code": "...", "message": "..."}}`

See `docs/OUTPUT-CONTRACT.md` for the full spec.

`--output {json,raw}` (default `json`) selects the format; `--pretty` indents the
JSON. `--output raw` bypasses the envelope, printing one verbatim log line per
record (structured records fall back to compact JSON), for both batch and `-f`
streams. An invalid value is rejected with `UNSUPPORTED_OUTPUT`.

## Security model

The primary boundary is the credential's own scope (e.g. a logs-only k8s
ServiceAccount, a RAM policy granting only `log:GetLogStoreLogs`). The adapter
secondary allowlist is defense-in-depth. See `docs/SECURITY-MODEL.md`.
