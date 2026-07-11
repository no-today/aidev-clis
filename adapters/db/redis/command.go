package redis

import "strings"

// Class is the security class of a redis command.
type Class int

const (
	ClassRead  Class = iota // safe reads — allowed
	ClassWrite              // mutating — needs --allow-write
	ClassAdmin              // dangerous/control-plane — refused unconditionally
)

func set(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

var readCmds = set(
	"GET", "MGET", "STRLEN", "GETRANGE", "GETBIT", "EXISTS", "TYPE", "TTL", "PTTL",
	"HGET", "HMGET", "HGETALL", "HKEYS", "HVALS", "HLEN", "HEXISTS", "HSTRLEN", "HSCAN",
	"LRANGE", "LLEN", "LINDEX", "LPOS",
	"SMEMBERS", "SCARD", "SISMEMBER", "SSCAN", "SRANDMEMBER",
	"ZRANGE", "ZREVRANGE", "ZCARD", "ZSCORE", "ZRANK", "ZREVRANK", "ZSCAN", "ZRANDMEMBER",
	"HRANDFIELD", "SUBSTR",
	"SCAN", "DBSIZE", "BITCOUNT", "DUMP", "ECHO", "PING", "INFO", "OBJECT",
	"XLEN", "XRANGE", "XREVRANGE", "XREAD", "XINFO",
)

// adminCmds are refused even with --allow-write: data-wiping, control-plane,
// arbitrary code, or O(n) blocking scans.
var adminCmds = set(
	"FLUSHALL", "FLUSHDB", "SWAPDB", "KEYS",
	"CONFIG", "SHUTDOWN", "DEBUG", "RESET", "FAILOVER",
	"SCRIPT", "EVAL", "EVALSHA", "FUNCTION",
	"MIGRATE", "CLIENT", "ACL", "CLUSTER", "MODULE",
	"SLAVEOF", "REPLICAOF", "SAVE", "BGSAVE", "BGREWRITEAOF", "LATENCY",
)

// Classify returns the security class of a command name (case-insensitive).
func Classify(cmd string) Class {
	c := strings.ToUpper(cmd)
	switch {
	case adminCmds[c]:
		return ClassAdmin
	case readCmds[c] || strings.HasSuffix(c, ".RO"):
		return ClassRead
	default:
		// Fail safe: an unrecognized command (incl. module commands) is treated
		// as a write, so it's gated behind --allow-write rather than running as
		// an implicit read.
		return ClassWrite
	}
}
