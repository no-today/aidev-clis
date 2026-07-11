# dbcli

dbcli gives an AI agent controlled, audited read (and opt-in write) access to
databases. A Go driver executes the SQL natively and returns a structured JSON
envelope; the credential is injected by the driver and never appears in the AI's
context or output.

## Synopsis

```
dbcli <driver> [--target <name>] [--database <db>] [--allow-write] "<sql>"
dbcli <driver> [--target <name>] databases | tables [<db>] | describe [<db>.]<table> | doctor
dbcli <driver> [--target <name>] insert [--table <name>] "<SELECT ...>"
dbcli targets
```

- `<driver>` — which engine to talk to; must match the target's `adapter`.
- `--target <name>` — which configured instance (credential + DSN).
- `--database <db>` — default namespace to scope the query/discovery to.
- `--allow-write` — permit a writing statement (DML). DDL is never permitted.

| driver | status | mechanism |
|---|---|---|
| `mysql` | **works** | go-sql-driver, native execution |
| `postgres` / `kingbase` | **works** | pgx (shared pgwire dialect) |
| `redis` | **works** | go-redis, command allowlist |
| `sqlite` | **works** | local file via modernc (pure Go) |
| `mongo` | **works** | mongosh statements via mongo-driver |
| `dataease` | **works** | last-resort read-only HTTP bypass via DataEase `sqlPreview` |

> **dataease is SELECT-only** — enforced by the DataEase backend itself
> (`sqlPreview` accepts a SELECT only; even `EXPLAIN` is rejected). It also
> supports the local verbs `insert` (renders a SELECT's rows as INSERT
> statements) and `doctor`. Catalog verbs (`databases`/`tables`/`describe`)
> are not available.

`kingbase` reuses the `postgres` path (KingbaseES speaks the pg wire protocol),
differing only by excluding its Oracle-compat system schemas (`sys`, `sysaudit`,
`oracle`, `sys_catalog`) from discovery. There is no public kingbase docker image: the pgwire
path is integration-tested against postgres, the delta is unit-tested, and a real
instance is verified with `go test -tags smoke` (`AIDEV_SMOKE_DSN`).

`sqlite` targets a local file — no credential, no tunnel, no server-side KILL. The
DSN is a `file:` URI (e.g. `file:/var/lib/app.db`) or a bare path. Open the file
read-only by appending `?mode=ro`; that is the primary security boundary for
sqlite, since there is no scoped-account model. The database namespace is always
`main` (SQLite's default attached database name).

## Discovering targets / schema

`dbcli targets` lists configured targets (name/adapter/description) — no `--target`, no
DB call. Then the three discovery verbs drill down:

```
dbcli mysql --target app databases               # the namespaces (mysql databases)
dbcli mysql --target app tables [<db>] [--like p] # tables in a namespace
dbcli mysql --target app describe [<db>.]<table>  # columns + indexes + comment/charset/collation/extra
dbcli mysql --target app doctor                   # staged health probe (see below)
```

`databases`/`tables`/`describe`/`doctor` are not row-capped. A bare `describe
orders` that matches a table in more than one namespace returns `TABLE_AMBIGUOUS`
with the candidates — qualify it as `describe <db>.orders`.

**`doctor`** is a staged health probe that emits a flat
`{"data":[{"name","ok","detail"},...]}` and exits non-zero if a stage fails:
`config` → `ssh` (only when the target has an `ssh:` bastion — it isolates a tunnel
failure from a DB failure) → `connect` (the ping). So a broken bastion shows
`ssh: ok=false` while the DB itself shows `connect: ok=false`.

### Namespace model

dbcli presents one namespace level, called `database`. For mysql it is the
database (one connection sees all the account's databases). For postgres/kingbase it is
the schema — the outer postgres database is fixed per target in the DSN (one business DB
per target). `database.table` qualification passes through to the engine natively.

## Query / write

```
dbcli mysql --target app "SELECT id, name FROM users WHERE status='NEW'"
dbcli mysql --target app --database billing "SELECT * FROM invoices"
dbcli mysql --target app --allow-write "UPDATE orders SET status='PAID' WHERE id=2"
```

- **Reads** (`SELECT`/`SHOW`/`EXPLAIN`/`WITH`) return `{"data":{"columns":[...],"rows":[[...]]}}`.
  The auto-LIMIT applies to `SELECT`/`WITH`: no `LIMIT` → `LIMIT 100` appended
  (default sample); an explicit `LIMIT` up to 100 is honored; **above 100 it
  errors** (`LIMIT_TOO_LARGE`) rather than silently truncating — paginate with
  `LIMIT 100 OFFSET <n>` or aggregate for more. `SHOW`/`EXPLAIN` pass through
  unmodified. Over-256-char cells add a `warnings` entry.
- **Writes** (`INSERT`/`UPDATE`/`DELETE`) require `--allow-write` and return
  `{"data":{"affected":N}}`. A statement that looks read-only but has side effects
  (`SELECT ... INTO`, `FOR UPDATE`, `nextval()`, `pg_sleep()`, `EXPLAIN ANALYZE`,
  a mutating `WITH`) is treated as a write.
- **DDL** (`CREATE`/`DROP`/`ALTER`/`TRUNCATE`/`GRANT`/`REVOKE`) is refused
  unconditionally, even with `--allow-write`.

Values are JSON-coerced: `int64` beyond 2⁵³ → string (precision), binary →
base64, `NULL` → `null`, time → RFC3339.

## Export rows as INSERT statements

```
dbcli mysql --target app insert "SELECT id, name FROM users WHERE id < 10"
dbcli postgres --target app insert --table public.users "SELECT * FROM users_view"
```

`insert` runs a read-only `SELECT` and prints each result row as a replayable
`INSERT INTO ... VALUES (...);` to stdout — **raw SQL, not the JSON envelope**
(errors still use the JSON error envelope). One statement per row.

- **Target table** is inferred from the `FROM` clause. Pass `--table <name>` to
  override; it is *required* when the `SELECT` joins, comma-lists tables, uses a
  subquery, or has no `FROM` — otherwise dbcli errors `INSERT_NO_TABLE`.
- **Columns** are the `SELECT`'s result columns.
- Read-only and auto-`LIMIT 100` like any read (an explicit `LIMIT` up to 100 is
  honored); a write/DDL `SELECT`-imposter is refused (`WRITE_NOT_ALLOWED`).
- Values render as native SQL literals (not JSON-coerced): `NULL`, unquoted
  numbers, quoted/escaped strings, RFC3339 timestamps, and dialect-specific blob
  (`0x…` on mysql, `'\x…'` elsewhere) and boolean (`1/0` vs `TRUE/FALSE`) forms.
- SQL-family drivers only (`mysql`/`postgres`/`kingbase`/`sqlite`); `redis`/`mongo` are
  not SQL and reject it.

```sql
INSERT INTO `users` (`id`, `name`) VALUES (1, 'alice');
INSERT INTO `users` (`id`, `name`) VALUES (2, 'bob');
```

## Diagnosing locks / killing stuck sessions

No special verb — it is plain SQL. **Viewing** is a read (no flag); **killing** a
session is a write (needs `--allow-write`) and requires a DB account with the
privilege, so run it from a dedicated higher-privilege `ops` target, keeping your
day-to-day target read-only.

```
# mysql — view, then kill (account needs PROCESS + CONNECTION_ADMIN)
dbcli mysql --target ops "SHOW PROCESSLIST"
dbcli mysql --target ops "SELECT * FROM sys.innodb_lock_waits"   # 8.0: performance_schema.data_lock_waits
dbcli mysql --target ops --allow-write "KILL QUERY 1542"

# postgres — view, then cancel/terminate (account needs the pg_signal_backend role)
dbcli postgres --target ops "SELECT pid, state, query FROM pg_stat_activity WHERE state='active'"
dbcli postgres --target ops "SELECT pid, pg_blocking_pids(pid) FROM pg_stat_activity WHERE wait_event_type='Lock'"
dbcli postgres --target ops --allow-write "SELECT pg_cancel_backend(48213)"      # cancel the query
dbcli postgres --target ops --allow-write "SELECT pg_terminate_backend(48213)"   # drop the connection
```

`pg_cancel_backend` / `pg_terminate_backend` are classified as writes (they have
side effects) even though they start with `SELECT`. The read-only daily account
cannot kill anything — that is the boundary working as intended.

## redis

redis is a command pass-through with the same three-tier guard, plus the verbs
mapped to redis semantics:

```
dbcli redis --target cache GET session:abc
dbcli redis --target cache HGETALL user:1
dbcli redis --target cache --allow-write SET k v        # write needs --allow-write
dbcli redis --target cache databases                    # logical DB numbers
dbcli redis --target cache tables [--like 'sess:%']     # SCAN keys (capped at 100)
dbcli redis --target cache describe user:1              # type / ttl / size / fields
dbcli redis --target cache doctor                       # PING
```

- **Read** commands (`GET`/`HGETALL`/`LRANGE`/`SCAN`/`TTL`/…) are allowed.
- **Write** commands (`SET`/`DEL`/`EXPIRE`/`HSET`/…) need `--allow-write` and
  return `{"data":{"affected":N}}`.
- **Admin/dangerous** commands are refused unconditionally — including `KEYS`
  (`KEYS *` is O(n) and blocks redis; use `tables`/`SCAN`), plus `FLUSHALL`,
  `CONFIG`, `SHUTDOWN`, `EVAL`, `CLIENT`, …
- The "database" is the numeric logical DB (from the DSN, or `--database <n>`).
  Provision a read-only ACL user as the primary boundary, same as the SQL drivers.

## mongo

mongo is **not** SQL — you write a native mongosh statement and dbcli parses it
(it does not run a JS engine), guards by method, and runs it:

```
dbcli mongo --target x 'db.users.find({status:"active"}).limit(50).sort({age:-1})'
dbcli mongo --target x 'db.orders.aggregate([{$group:{_id:"$status",n:{$sum:1}}}])'
dbcli mongo --target x 'db.users.countDocuments({active:true})'
dbcli mongo --target x --allow-write 'db.users.updateOne({_id:ObjectId("...")},{$set:{x:1}})'
dbcli mongo --target x databases | tables | describe <coll> | doctor
```

- **Reads** (`find`/`aggregate`/`countDocuments`/…) are allowed; output is a JSON
  **document array** `{"data":[{...},...]}`, with `find` capped at 100 by default,
  `.limit(N)` up to 100 honored, above 100 `LIMIT_TOO_LARGE`. Per-method output
  shapes are in `dbcli mongo -h`.
- **Writes** (`insertOne`/`updateOne`/`deleteOne`/…) need `--allow-write` and
  return `{"data":{"affected":N}}`. An `aggregate` whose pipeline contains
  `$out`/`$merge` (even nested in `$facet`/`$lookup`) is treated as a write.
- **Destructive** methods (`drop`/`createIndex`/`renameCollection`/unknown) are
  refused (`MONGO_METHOD_REFUSED`). Server-side JavaScript
  (`$where`/`$function`/`$accumulator`, anywhere in a filter or pipeline) is
  refused outright (`MONGO_JS_REFUSED`) — it's an arbitrary-code/DoS surface even
  in a read.
- Long string fields are truncated to 256 characters (with `…`); a `field(s)
  truncated to 256 chars` warning is emitted so a multi-MB document can't blow
  the context.
- The relaxed parser accepts unquoted keys, single/double quotes, and shell
  helpers (`ObjectId`/`ISODate`/`NumberLong`/`UUID`/regex/…). The database is the
  one in the URI (or `--database`); `db.<coll>` selects the collection.
- Provision a **read-only mongo user** as the primary boundary.

## Config

```yaml
# ~/.aidev-clis/dbcli.yaml
default_target: app
targets:
  app:
    adapter: mysql
    description: app db
    dsn: mysql://app_ro@10.0.0.1:3306/app   # URL DSN; password injected from credential
    credential: db.app.password              # ~/.aidev-clis/credentials/... (0600), the password only
```

TLS is off by default and configured **in the DSN query string**, not a separate
key: mysql `?tls=true|skip-verify|preferred` (go-sql-driver), postgres/kingbase
`?sslmode=require|verify-full` (or a top-level `sslmode:` field on the env).

To reach a DB behind a bastion, add an `ssh:` block (key, encrypted-key, or
password auth — exactly one of `identity_file` / `password_credential`):

```yaml
    ssh:
      host: bastion.example.com
      user: deploy
      port: 22                              # optional, default 22
      identity_file: ~/.ssh/id_ed25519      # key auth
      # key_passphrase_credential: ssh.b.pass     # if the key is encrypted (credstore key)
      # password_credential: ssh.b.password       # OR password auth (credstore key)
```

The DSN host is the DB address **as the bastion sees it**; dbcli opens a local
forward and routes the connection through it. Every networked driver supports the
`ssh:` tunnel — mysql, postgres, kingbase, redis, mongo; only `sqlite` (a local file)
has no endpoint to tunnel to.

## Security model

**Primary boundary (backend enforced):** inject a scoped account — a read-only DB
user. That account's own grants are the real boundary: even if a statement slips
past the guard, the database rejects an unauthorized write.

**Secondary (defense-in-depth, in dbcli):** the statement-class guard above plus
single-statement enforcement (no `;`-smuggled second statement). A query that
overruns its `--timeout` is cancelled; the cancellation mechanism is
driver-specific (e.g. SQLite, a local file with no server backend, is a no-op).
See `docs/SECURITY-MODEL.md`.

## Output contract

Success `{"data": <payload>}` (+ `warnings` when present); error
`{"error":{"code","message"}}`. Exit codes 0 ok / 1 general / 2 config / 3 auth /
4 timeout / 5 remote. See `docs/OUTPUT-CONTRACT.md`.

`--output {json,raw}` (default `json`) selects the format; `--pretty` indents the
JSON. `--output raw` bypasses the envelope: query rows as TSV (no header, pipe to
grep/awk/cut), scalar maps as `key=value`. An invalid value is rejected with
`UNSUPPORTED_OUTPUT`.
