# dbcli

Query databases (least-privilege, credential-hiding). See [docs/cli-dbcli.md](../../docs/cli-dbcli.md) for full reference.

## Synopsis

```
dbcli <driver> [--target <name>] [--database <db>] [--allow-write] [--timeout <dur>] [--config-dir <path>] [--pretty] "<sql>"
dbcli <driver> [--target <name>] databases | tables [<db>] | describe [<db>.]<table> | doctor
dbcli <driver> [--target <name>] insert [--table <name>] "<SELECT ...>"
dbcli targets
```

## Verbs

### databases / tables / describe / doctor

Schema-discovery and health probes. See [docs/cli-dbcli.md](../../docs/cli-dbcli.md#discovering-targets--schema).

### insert — export a read as INSERT statements

Runs a read-only SELECT and prints each result row as a replayable `INSERT INTO ... VALUES (...);` on stdout (raw SQL, **not** the JSON envelope). Errors still use the JSON error envelope.

```
dbcli mysql --target dev insert "SELECT id, name FROM users WHERE id < 10"
```

Sample output:

```sql
INSERT INTO `users` (`id`, `name`) VALUES (1, 'alice');
INSERT INTO `users` (`id`, `name`) VALUES (2, 'bob');
```

- The target table is inferred from the FROM clause. Pass `--table <name>` to override — required when the SELECT joins, comma-lists tables, uses a subquery, or has no FROM (otherwise it errors `INSERT_NO_TABLE`).
- The INSERT column list is taken from the SELECT's result columns.
- Read-only and auto-`LIMIT 100` like any read; a write or DDL statement is refused.
- SQL-family drivers only: mysql, postgres, kingbase, sqlite. One statement per row.
