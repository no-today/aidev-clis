package mongo

import (
	"encoding/base64"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// coerce converts a decoded BSON value into a clean JSON-friendly value so the
// AI gets plain JSON, not extended-JSON wrappers.
func coerce(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case primitive.ObjectID:
		return x.Hex()
	case primitive.DateTime:
		return x.Time().UTC().Format(time.RFC3339)
	case primitive.Timestamp:
		return map[string]any{"t": x.T, "i": x.I}
	case primitive.Decimal128:
		return x.String()
	case primitive.Binary:
		return base64.StdEncoding.EncodeToString(x.Data)
	case primitive.Regex:
		return "/" + x.Pattern + "/" + x.Options
	case primitive.MinKey:
		return "MinKey"
	case primitive.MaxKey:
		return "MaxKey"
	case primitive.Null, primitive.Undefined:
		return nil
	case int64:
		if x > 1<<53 || x < -(1<<53) {
			return strconv.FormatInt(x, 10)
		}
		return x
	case bson.M:
		m := make(map[string]any, len(x))
		for k, val := range x {
			m[k] = coerce(val)
		}
		return m
	case bson.D:
		m := make(map[string]any, len(x))
		for _, e := range x {
			m[e.Key] = coerce(e.Value)
		}
		return m
	case bson.A:
		return coerceSlice(x)
	case []any:
		return coerceSlice(x)
	default:
		return v
	}
}

func coerceSlice(xs []any) []any {
	out := make([]any, len(xs))
	for i, v := range xs {
		out[i] = coerce(v)
	}
	return out
}

// cellCap is the per-string character cap (same contract as the SQL/redis
// drivers): a multi-MB document field must not blow the AI's context.
const cellCap = 256

// capStrings recursively truncates any string longer than max runes (appending
// "…") in a coerced result (maps/slices/strings), returning whether anything was
// cut. Mutates maps/slices in place.
func capStrings(v any, max int) (any, bool) {
	switch x := v.(type) {
	case string:
		r := []rune(x)
		if len(r) > max {
			return string(r[:max]) + "…", true
		}
		return x, false
	case map[string]any:
		cut := false
		for k, val := range x {
			nv, c := capStrings(val, max)
			x[k] = nv
			cut = cut || c
		}
		return x, cut
	case []any:
		cut := false
		for i, val := range x {
			nv, c := capStrings(val, max)
			x[i] = nv
			cut = cut || c
		}
		return x, cut
	default:
		return v, false
	}
}
