// Package redis is the dbcli "redis" driver: a command pass-through with a
// three-tier guard (read / write-needs--allow-write / admin-refused). It is
// standalone (redis is not SQL) — connection + tunnel come from dbconn.
package redis

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/dbcli"
	"github.com/no-today/aidev-clis/internal/dbcli/dbconn"
)

type adapter struct{}

func New() dbcli.Driver { return adapter{} }

func (adapter) Name() string { return "redis" }

func (a adapter) Run(ctx context.Context, in dbcli.Input, out dbcli.Output) error {
	args := in.Args
	// the user may quote the whole command ("GET k") → split it.
	if len(args) == 1 && strings.ContainsAny(args[0], " \t") {
		args = splitCommand(args[0])
	}
	if len(args) == 0 {
		return errs.Config("EMPTY_COMMAND", "no redis command provided")
	}

	client, cleanup, err := connect(ctx, in)
	if err != nil {
		return err
	}
	defer cleanup()

	switch strings.ToLower(args[0]) {
	case "databases":
		return listDatabases(ctx, client, out)
	case "tables":
		return listTables(ctx, client, in, args, out)
	case "describe":
		if len(args) < 2 {
			return errs.Config("DESCRIBE_NO_KEY", "describe requires a key")
		}
		return describeKey(ctx, client, args[1], out)
	case "doctor":
		if err := client.Ping(ctx).Err(); err != nil {
			return wrapErr("REDIS_PING", err)
		}
		return out.Batch(map[string]any{"ok": true})
	default:
		return runCommand(ctx, client, args, in.AllowWrite, out)
	}
}

func connect(ctx context.Context, in dbcli.Input) (*goredis.Client, func(), error) {
	u, cleanup, err := dbconn.Resolve(ctx, in.Target)
	if err != nil {
		return nil, nil, err
	}
	opt, err := goredis.ParseURL(u.String())
	if err != nil {
		cleanup()
		return nil, nil, errs.Config("REDIS_DSN_INVALID", dbconn.Redact(err.Error(), u))
	}
	if in.Database != "" {
		n, err := strconv.Atoi(in.Database)
		if err != nil || n < 0 {
			cleanup()
			return nil, nil, errs.Config("REDIS_DB_INVALID", "redis --database must be a non-negative integer")
		}
		opt.DB = n
	}
	client := goredis.NewClient(opt)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		cleanup()
		return nil, nil, dbconn.RedactErr(wrapErr("REDIS_CONNECT", err), u)
	}
	return client, func() { _ = client.Close(); cleanup() }, nil
}

func runCommand(ctx context.Context, client *goredis.Client, args []string, allowWrite bool, out dbcli.Output) error {
	verb := args[0]
	switch Classify(verb) {
	case ClassAdmin:
		return errs.Config("REDIS_ADMIN_REFUSED", "redis admin/dangerous command "+strings.ToUpper(verb)+" is never allowed")
	case ClassWrite:
		if !allowWrite {
			return errs.Config("WRITE_NOT_ALLOWED", "this command writes; pass --allow-write to permit it")
		}
	}

	value, err := doWithCancel(ctx, client, toAnyArgs(args))
	if err != nil && !errors.Is(err, goredis.Nil) {
		return wrapErr("REDIS_COMMAND", err)
	}
	if errors.Is(err, goredis.Nil) {
		value = nil
	}

	if Classify(verb) == ClassWrite {
		return out.Batch(map[string]any{"affected": affectedFromResult(verb, value)})
	}
	cols, rows := shapeResult(verb, value)
	var warnings []string
	// Bound a multi-element reply (LRANGE/SMEMBERS/HGETALL/ZRANGE/…) to rowCap
	// elements: a huge structure must not emit unbounded rows into the envelope.
	// A scalar reply (GET, etc.) is a single row, so this never touches it.
	if len(rows) > rowCap {
		rows = rows[:rowCap]
		warnings = append(warnings, "result truncated to 100 elements")
	}
	if truncateRows(rows, cellCap) {
		warnings = append(warnings, "cell(s) truncated to 256 chars")
	}
	return out.Batch(dbcli.Result{Columns: cols, Rows: rows}, warnings...)
}

// cellCap is the per-cell character cap (mirrors sqlcore; a fat GET/HGET value
// must not blow the AI's context, same contract as the SQL drivers).
const cellCap = 256

// rowCap is the max elements returned from a multi-element read reply (mirrors
// sqlcore.DefaultRowCap and the `tables` verb's scanCap — same 100-row default
// across the suite).
const rowCap = 100

// splitCommand splits a whole-command string into tokens, honoring simple
// double-quoted segments so a quoted value with spaces (SET k "a b") stays a
// single token. It is deliberately minimal — NOT a shell parser: no single
// quotes, no escapes. Pass anything fancier as separate argv elements.
func splitCommand(s string) []string {
	var tokens []string
	var cur strings.Builder
	inQuote, started := false, false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
			started = true
		case !inQuote && (r == ' ' || r == '\t'):
			if started {
				tokens = append(tokens, cur.String())
				cur.Reset()
				started = false
			}
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	if started {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

func toAnyArgs(args []string) []any {
	out := make([]any, len(args))
	for i, a := range args {
		out[i] = a
	}
	return out
}

// doWithCancel runs the command, issuing CLIENT KILL on a fresh conn if ctx is
// cancelled before it returns (timeout-KILL for runaway commands).
func doWithCancel(ctx context.Context, client *goredis.Client, args []any) (any, error) {
	if ctx.Done() == nil {
		return client.Do(ctx, args...).Result()
	}
	id, err := client.ClientID(ctx).Result()
	if err != nil {
		return client.Do(ctx, args...).Result() // best-effort: no id, just run
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-ctx.Done():
			cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = client.ClientKillByFilter(cctx, "ID", strconv.FormatInt(id, 10)).Err()
		case <-stop:
		}
	}()
	res, err := client.Do(ctx, args...).Result()
	if ctx.Err() == nil {
		close(stop)
	}
	<-done
	return res, err
}

func wrapErr(code string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return errs.Timeout("REDIS_TIMEOUT", err.Error())
	}
	return errs.Remote(code, err.Error())
}
