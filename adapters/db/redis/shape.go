package redis

import (
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// shapeResult maps a redis reply to columns + rows, keyed off the command.
func shapeResult(command string, value any) ([]string, [][]any) {
	switch strings.ToUpper(command) {
	case "HGETALL":
		return shapeMap(value)
	case "LRANGE", "SMEMBERS", "HKEYS", "HVALS", "ZRANGE", "ZREVRANGE", "MGET", "HMGET":
		return shapeIndexed(value)
	case "SCAN", "SSCAN", "HSCAN", "ZSCAN":
		return shapeScan(value)
	default:
		return shapeScalar(value)
	}
}

func shapeMap(value any) ([]string, [][]any) {
	switch m := value.(type) {
	case map[any]any:
		keys := make([]string, 0, len(m))
		vals := map[string]any{}
		for k, v := range m {
			ks := fmt.Sprint(k)
			keys = append(keys, ks)
			vals[ks] = v
		}
		sort.Strings(keys)
		rows := make([][]any, 0, len(keys))
		for _, k := range keys {
			rows = append(rows, []any{k, normalizeValue(vals[k])})
		}
		return []string{"field", "value"}, rows
	case map[string]string:
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		rows := make([][]any, 0, len(keys))
		for _, k := range keys {
			rows = append(rows, []any{k, m[k]})
		}
		return []string{"field", "value"}, rows
	default:
		return shapeScalar(value)
	}
}

func shapeIndexed(value any) ([]string, [][]any) {
	items := sliceValues(value)
	rows := make([][]any, 0, len(items))
	for i, item := range items {
		rows = append(rows, []any{int64(i), normalizeValue(item)})
	}
	return []string{"index", "value"}, rows
}

func shapeScan(value any) ([]string, [][]any) {
	items := sliceValues(value)
	if len(items) == 2 {
		cursor := normalizeValue(items[0])
		var rows [][]any
		for _, item := range sliceValues(items[1]) {
			rows = append(rows, []any{cursor, normalizeValue(item)})
		}
		return []string{"cursor", "value"}, rows
	}
	return shapeIndexed(value)
}

func shapeScalar(value any) ([]string, [][]any) {
	switch value.(type) {
	case []any, []string:
		return shapeIndexed(value)
	case map[any]any, map[string]string:
		return shapeMap(value)
	default:
		return []string{"value"}, [][]any{{normalizeValue(value)}}
	}
}

// normalizeValue makes a redis reply JSON-friendly: []byte → string (or base64
// when binary), big int64 → string (JS precision), nested maps recursed.
func normalizeValue(value any) any {
	switch v := value.(type) {
	case []byte:
		if utf8.Valid(v) {
			return string(v)
		}
		return base64.StdEncoding.EncodeToString(v)
	case int64:
		if v > 1<<53 || v < -(1<<53) {
			return strconv.FormatInt(v, 10)
		}
		return v
	case map[any]any:
		out := map[string]any{}
		for k, item := range v {
			out[fmt.Sprint(k)] = normalizeValue(item)
		}
		return out
	default:
		return v
	}
}

func sliceValues(value any) []any {
	switch v := value.(type) {
	case []any:
		return v
	case []string:
		out := make([]any, len(v))
		for i, s := range v {
			out[i] = s
		}
		return out
	default:
		return []any{v}
	}
}

// truncateRows truncates any string cell longer than max runes (appending "…"),
// mutating rows in place; returns whether anything was cut. (Parallels
// sqlcore.TruncateCells — kept local so redis stays standalone.)
func truncateRows(rows [][]any, max int) bool {
	cut := false
	for i := range rows {
		for j, c := range rows[i] {
			s, ok := c.(string)
			if !ok {
				continue
			}
			r := []rune(s)
			if len(r) > max {
				rows[i][j] = string(r[:max]) + "…"
				cut = true
			}
		}
	}
	return cut
}

// redisNonAffectedVerbs reply with int/"OK" that does NOT mean rows-affected.
var redisNonAffectedVerbs = set("PUBLISH", "FLUSHDB", "FLUSHALL", "CONFIG", "DEBUG", "SHUTDOWN", "MIGRATE", "RESET")

// affectedFromResult derives a rows-affected count for a write reply.
func affectedFromResult(verb string, value any) int64 {
	if redisNonAffectedVerbs[strings.ToUpper(verb)] {
		return 0
	}
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case bool:
		if v {
			return 1
		}
		return 0
	case string:
		if strings.EqualFold(v, "OK") || strings.EqualFold(v, "QUEUED") {
			return 1
		}
		return 0
	case []any:
		return int64(len(v))
	default:
		return 0
	}
}
