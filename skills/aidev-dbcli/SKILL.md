---
name: aidev-dbcli
description: Use when an AI agent needs to read (or, with explicit intent, write) a database — running SQL, listing databases/tables/columns, diagnosing locks, or checking connectivity — across configured targets (mysql / postgres / kingbase / redis / sqlite / mongo; plus dataease, a last-resort read-only HTTP bypass). Covers the read/write guard, the JSON envelope, the namespace model, and target inference.
---

# dbcli

`dbcli` gives an AI agent controlled, audited database access. You pass plain SQL
(or a redis command); the CLI injects a scoped credential you never see, runs it
natively, and returns a minimal JSON envelope. Drivers: `mysql`, `postgres`,
`kingbase`, `redis`, `sqlite` (local file, `?mode=ro` for read-only boundary —
also the right tool for a project-local .sqlite/.db file: add a target pointing
at the path instead of shelling raw `sqlite3`),
`mongo` (mongosh statements, see below). Plus `dataease` — a **last-resort**
read-only HTTP bypass, only when a DB can't be reached directly (see below);
prefer a direct driver whenever the network allows.

```
dbcli <driver> [--target <name>] [--database <db>] [--allow-write] [--timeout 30s] [--pretty] "<sql>"
dbcli <driver> [--target <name>] databases | tables [<db>] [--like <pat>] | describe [<db>.]<table> | doctor
dbcli <driver> [--target <name>] insert [--table <name>] "<SELECT ...>"   # SQL drivers: render rows as INSERT statements
dbcli targets
```

The driver must match the target's `adapter` (else `ADAPTER_MISMATCH`). Output is the
JSON envelope by default, which is already what an agent wants — **don't pass
`--output`**. (`--output raw` drops the envelope to TSV rows / `key=value`, and
`--pretty` indents — both are for humans piping/reading, not for agents.)

## Output contract (parse this)

- Read → `{"data":{"columns":["id","name"],"rows":[[1,"a"],...]}}`. Rows align to
  `columns` by index; cells truncate at 256 chars (adds a `"warnings"`). Row
  count rules:
  - **No `LIMIT` → you get 100** (a default sample, `LIMIT 100` is appended).
  - **An explicit `LIMIT N` up to 100 is honored** — write `LIMIT 50` to get 50.
  - **`LIMIT N` above 100 is rejected** (`LIMIT_TOO_LARGE`, exit 2) — not
    silently truncated. For more than 100, **paginate** with `LIMIT 100 OFFSET
    <n>`, or aggregate (`COUNT`/`GROUP BY`). dbcli is not a bulk-export tool.
- Write → `{"data":{"affected":N}}`.
- `describe` → `{"data":{"database","table","comment","charset","collation","columns":[{name,type,nullable,key,default,comment,charset,collation,extra}],"indexes":[...]}}`.
  Most fields are `omitempty`: `comment` is the column/table remark; `charset`/`collation`
  are mysql-only at table level and on text columns (pg surfaces `collation` only);
  `extra` carries `auto_increment` / `on update ...` (mysql) or `identity` / `generated`
  (pg). pg `type` is normalized to carry length/precision (`character varying(64)`,
  `numeric(10,2)`).
- Error → `{"error":{"code":"...","message":"..."}}`. Exit codes co-signal:
  `0` ok · `1` general · `2` config/guard · `3` auth · `4` timeout · `5` remote.
- Values: `int64` beyond 2⁵³ → string (precision), binary → base64, NULL → `null`.

## Read vs write (the guard — second layer; the scoped account is the first)

- **Reads run freely:** `SELECT` / `SHOW` / `EXPLAIN` / `DESCRIBE` / `WITH`.
- **Writes need `--allow-write`:** `INSERT` / `UPDATE` / `DELETE`. Returns
  `WRITE_NOT_ALLOWED` (exit 2) without it. A statement that *looks* read-only but
  has side effects (`SELECT ... INTO`, `FOR UPDATE`, `nextval()`, `pg_sleep()`,
  `EXPLAIN ANALYZE`, a mutating `WITH`) is treated as a write.
- **DDL is always refused** (`DDL_REFUSED`), even with `--allow-write`:
  `CREATE`/`DROP`/`ALTER`/`TRUNCATE`/`GRANT`/`REVOKE`. Use the project's
  migration tool + git, not dbcli, for schema change.
- Only one statement per call (`;`-smuggled second statements are rejected).

Only set `--allow-write` after the user has confirmed write intent. Even then,
the backend account may lack permission (that rejection is the design working).

## Discovery — drill down before querying

You usually don't know the schema. Find it cheaply (these are not row-capped):

```
dbcli targets                                  # which targets exist (name/adapter/description)
dbcli mysql --target app databases             # the namespaces
dbcli mysql --target app tables [<db>] [--like 'order%']
dbcli mysql --target app describe orders       # columns + indexes
dbcli mysql --target app doctor                # staged probe: config → [ssh] → connect
```

## insert — render rows as INSERT statements (SQL drivers only)

`dbcli <sql-driver> insert [--table <name>] "<SELECT ...>"` runs the SELECT
(read-only, same guard/auto-LIMIT) and emits the result rows as dialect-correct
`INSERT INTO <table> (...) VALUES (...)` text instead of columns/rows — for
copying data between targets or seeding. The table is inferred from the query's
`FROM` (override with `--table`; `INSERT_NO_TABLE` if neither resolves). Literals
are quoted per the driver's dialect. SQL family only — `redis`/`mongo` reject it.

## PII hygiene — query results are not seed data

Real PII in query results (national ID numbers, phone numbers, bank/card
numbers, names) must never be copied into seed data, test cases, documentation,
or persistent notes/memory — it stays in the conversation.

- Refer to a person/account by a semantics-free surrogate key (auto-increment
  PK, internal id), never by an identity/document number.
- Seed and test data: generate format-valid but obviously fake values; never
  reuse values queried from a real database (`insert` output included).
- Filtering on a PII value the user supplied (`WHERE id_no = ...`) is fine —
  pass it through; don't SELECT PII out first and spread it further.

## Namespace model (`database` = the qualifier namespace)

dbcli presents ONE namespace level called `database`:

- **mysql:** it is the database; one env sees all the account's databases.
- **postgres / kingbase:** it is the **schema**. The outer postgres database is fixed per target
  in the DSN (one business DB per target) — to reach another postgres database, use another
  target, not `--database`.
- `--database <x>` scopes the query/discovery (mysql: the db; postgres: `search_path`).
  Qualify inline as `database.table` in SQL — works natively in both.
- `describe orders` when `orders` lives in two namespaces → `TABLE_AMBIGUOUS`
  with the candidates; re-run `describe <db>.orders`.

## redis

A command pass-through with the same three tiers, plus verbs:

```
dbcli redis --target cache GET session:abc            # read
dbcli redis --target cache --allow-write SET k v       # write needs --allow-write
dbcli redis --target cache tables [--like 'sess:%']    # SCAN keys (capped 100)
dbcli redis --target cache describe user:1            # type / ttl / size / fields
dbcli redis --target cache databases | doctor
```

`KEYS` is **refused** (admin) — `KEYS *` blocks redis; use `tables`/`SCAN`.
Also refused: `FLUSHALL`/`CONFIG`/`SHUTDOWN`/`EVAL`/`CLIENT`. The redis
"database" is the numeric logical DB (in the DSN, or `--database <n>`).

## mongo (NOT SQL — native mongosh statements)

Write the mainstream mongosh form; dbcli parses it (no JS engine), guards by
method, runs it. Output is a **document array**, not columns+rows.

```
dbcli mongo --target x 'db.users.find({status:"active"}).limit(50).sort({age:-1})'
dbcli mongo --target x 'db.orders.aggregate([{$group:{_id:"$status",n:{$sum:1}}}])'
dbcli mongo --target x 'db.users.countDocuments({active:true})'
dbcli mongo --target x --allow-write 'db.users.updateOne({_id:ObjectId("..")},{$set:{x:1}})'
dbcli mongo --target x databases | tables | describe <coll> | doctor
```

- Reads (`find`/`aggregate`/`count`/`distinct`) free → `{"data":[{...}]}`; writes
  (`insertOne`/`updateOne`/`deleteOne`/…) need `--allow-write` → `{"data":{"affected":N}}`.
- `drop`/`createIndex`/unknown methods are refused; an `aggregate` with
  `$out`/`$merge` counts as a write. Server-side JS
  (`$where`/`$function`/`$accumulator`) is refused outright (`MONGO_JS_REFUSED`).
- `find` caps at 100; `.limit(N)` up to 100; above 100 errors — paginate with
  `.skip().limit()`. The db is in the URI (or `--database`).

## dataease (last resort — read-only HTTP bypass)

Use **only** when the target DB can't be reached directly but its DataEase web
service can. Not a general driver: no direct connection, translates a read-only
SQL into a DataEase `sqlPreview` request over HTTP.

```
dbcli dataease --target de "select * from orders limit 50"   # read only
dbcli dataease --target de doctor                            # auth + connectivity probe
dbcli dataease --target de insert [--table t] [--exclude a,b] "<SELECT ...>" # rows as INSERTs
```

- **Read-only**: no exec, no writes (`--allow-write` is a no-op); `databases`/
  `tables`/`describe` are unsupported (`DATAEASE_UNSUPPORTED_VERB`). Only a SELECT,
  `insert`, and `doctor`. SQL goes verbatim to `sqlPreview` (no auto-LIMIT — add
  your own `LIMIT`).
- **insert**: renders the SELECT's rows as `INSERT` statements (read-only text,
  doesn't write). DataEase returns values untyped (all strings), so output is
  **MySQL-flavored and every non-null value is quoted as a string** (MySQL coerces
  on insert; for strict-mode/non-MySQL targets you must adjust). Table inferred
  from `FROM` or pass `--table`; JOIN/subquery → `INSERT_NO_TABLE`. Drop columns
  with `--exclude a,b,c` (case-insensitive; e.g. omit an auto-increment PK).
- **Session + auto-login**: token persisted at `~/.aidev-clis/sessions/<session>`,
  bound to `base_url`. With `login_credential` set, an expired/missing session
  triggers one re-login + retry automatically.
- **Errors**: `DATAEASE_AUTH_EXPIRED` (re-login fires if a credential is set),
  `DATAEASE_WAF_BLOCKED` (backend WAF rejected the request — not retryable here),
  `DATAEASE_QUERY_FAILED` (DataEase rejected the SQL).
- Config keys: `base_url`, `data_source_id` (required), `session`,
  `login_credential`, `timeout_seconds`.

The bundled `dbcli.yaml` (alongside this skill) is a fully-commented reference
template covering every driver — copy it to `~/.aidev-clis/dbcli.yaml` and fill in
your targets/credentials (real configs never leave the machine). You can offer to
write it for the user from this sample.

## Diagnosing locks / killing stuck sessions

Plain SQL — viewing is a read, killing is a write needing `--allow-write` AND a
privileged `ops` target (the read-only daily account can't kill, by design):

```
dbcli mysql --target ops "SHOW PROCESSLIST"
dbcli mysql --target ops --allow-write "KILL QUERY 1542"
dbcli postgres --target ops "SELECT pid, state, query FROM pg_stat_activity WHERE state='active'"
dbcli postgres --target ops --allow-write "SELECT pg_terminate_backend(48213)"
```

## Big SQL — pass a file, not a 30KB argv

For long scripts (offline-report SQL, generated INSERTs), skip the shell-quoting
scaffolding: `--file q.sql` (or `--file -` for stdin) reads the statement from a
file and is equivalent to the inline form. `EXPLAIN <INSERT/UPDATE ...>` (without
ANALYZE) classifies as a read — it is the sanctioned way to validate a write
script without executing it.

## Infer the target before running

Don't blindly use `default_target` for a remote DB. Prefer an explicit user target, the
release/UAT/dev context, or service/table names. `dbcli targets` is a safe local
read to list candidates. If several targets fit — or the user just says "prod"/"the
database" — ask one short confirmation before running, especially before any
`--allow-write`.

Target resolution is adapter-aware: if the driver has **exactly one** configured
target, `--target` is optional (it's auto-selected). If it has several, omitting
`--target` returns `TARGET_AMBIGUOUS` listing the candidates — pick one explicitly.

## Config

```yaml
# ~/.aidev-clis/dbcli.yaml
default_target: app
targets:
  app:
    adapter: mysql
    description: app db
    dsn: mysql://app_ro@10.0.0.1:3306/app   # URL DSN; password injected from credential
    credential: db.app.password              # ~/.aidev-clis/credentials/... (0600), password only
    ssh:                                     # optional: reach a DB behind a bastion
      host: bastion.example.com
      user: deploy
      identity_file: ~/.ssh/id_ed25519       # or password_credential: ssh.b.password
```

The credential file is mode-0600 and holds only the password; the AI never sees
it. Provision a **read-only account** — that scope is the real security boundary.

For the shared `~/.aidev-clis` layout, the credentials model, and from-zero setup
steps, see the **use-aidev** skill.

## Don't use dbcli for

- Schema migrations / DDL — refused; use the migration tool + git.
- Bulk dumps / ETL / cross-DB sync — reads cap at 100 rows.
- Multi-statement transactions — one statement per call; use the app's API
  (`apicli`) or a deploy job (`jcli`).
- Local dev DBs — use the native client.
- An assertion you want kept + re-runnable with a PASS/FAIL verdict — a post-deploy gate or a cross-system regression invariant (e.g. row-level equivalence across sources) — use `tcli`.

## Audit

Every invocation (including rejected ones) appends to `~/.aidev-clis/audit/<YYYYMMDD>.jsonl`
(one dated file per UTC day; 30-day auto-prune) with the driver, target, full command,
`allow_write`, and outcome (`ok`/`error`). Write operations (`--allow-write`) are
two-phase: a `started` line before dispatch and a terminal line after share an `id`.
Credential values are never written.
