package redis

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/no-today/aidev-clis/internal/dbcli"
)

const scanCap = 100

// listDatabases lists the reachable logical DB numbers (CONFIG GET databases;
// falls back to ["0"] when CONFIG is unavailable).
func listDatabases(ctx context.Context, client *goredis.Client, out dbcli.Output) error {
	rows := [][]any{{"0"}}
	if vals, err := client.Do(ctx, "CONFIG", "GET", "databases").StringSlice(); err == nil && len(vals) == 2 {
		if max, err := strconv.Atoi(vals[1]); err == nil && max > 0 {
			rows = rows[:0]
			for i := 0; i < max; i++ {
				rows = append(rows, []any{strconv.Itoa(i)})
			}
		}
	}
	return out.Batch(dbcli.Result{Columns: []string{"database"}, Rows: rows})
}

// listTables SCANs keys (the `tables` verb), capped at scanCap. --like becomes a
// MATCH glob (% → *, _ → ?).
func listTables(ctx context.Context, client *goredis.Client, in dbcli.Input, args []string, out dbcli.Output) error {
	pattern := "*"
	for i := 1; i+1 < len(args); i++ {
		if args[i] == "--like" {
			pattern = strings.NewReplacer("%", "*", "_", "?").Replace(args[i+1])
		}
	}
	db := "0"
	if in.Database != "" {
		db = in.Database
	}
	var rows [][]any
	var cursor uint64
	for len(rows) < scanCap {
		keys, next, err := client.Scan(ctx, cursor, pattern, scanCap).Result()
		if err != nil {
			return wrapErr("REDIS_SCAN", err)
		}
		for _, k := range keys {
			rows = append(rows, []any{db, k})
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	var warnings []string
	if len(rows) >= scanCap {
		rows = rows[:scanCap]
		warnings = append(warnings, "result truncated to 100 keys; use --like to narrow")
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][1].(string) < rows[j][1].(string) })
	return out.Batch(dbcli.Result{Columns: []string{"database", "name"}, Rows: rows}, warnings...)
}

// describeKey returns a key's schema (type, ttl, size, hash fields).
func describeKey(ctx context.Context, client *goredis.Client, key string, out dbcli.Output) error {
	typ, err := client.Type(ctx, key).Result()
	if err != nil {
		return wrapErr("REDIS_DESCRIBE", err)
	}
	ttl, _ := client.TTL(ctx, key).Result()
	schema := map[string]any{
		"key":         key,
		"type":        typ,
		"exists":      typ != "none",
		"ttl_seconds": int64(ttl / time.Second),
	}
	if typ == "none" {
		return out.Batch(schema)
	}
	if enc, err := client.Do(ctx, "OBJECT", "ENCODING", key).Text(); err == nil {
		schema["encoding"] = enc
	}
	switch typ {
	case "string":
		schema["size"], _ = client.StrLen(ctx, key).Result()
	case "hash":
		schema["size"], _ = client.HLen(ctx, key).Result()
		if fields, err := client.HKeys(ctx, key).Result(); err == nil {
			sort.Strings(fields)
			schema["fields"] = fields
		}
	case "list":
		schema["size"], _ = client.LLen(ctx, key).Result()
	case "set":
		schema["size"], _ = client.SCard(ctx, key).Result()
	case "zset":
		schema["size"], _ = client.ZCard(ctx, key).Result()
	case "stream":
		schema["size"], _ = client.XLen(ctx, key).Result()
	}
	return out.Batch(schema)
}
