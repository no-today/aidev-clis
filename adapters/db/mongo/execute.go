package mongo

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

const (
	defaultDocCap = 100
	maxDocCap     = 100
)

// Execute runs the parsed statement against coll and returns the payload to emit
// plus any warnings. Guard/classification happen before this (in mongo.go).
func Execute(ctx context.Context, coll *mongo.Collection, st *Statement) (any, []string, error) {
	switch st.Method {
	case "find", "findOne":
		return runFind(ctx, coll, st)
	case "aggregate":
		return runAggregate(ctx, coll, st)
	case "countDocuments", "count":
		n, err := coll.CountDocuments(ctx, filterArg(st, 0))
		if err != nil {
			return nil, nil, wrapErr(err)
		}
		return map[string]any{"count": n}, nil, nil
	case "estimatedDocumentCount":
		n, err := coll.EstimatedDocumentCount(ctx)
		if err != nil {
			return nil, nil, wrapErr(err)
		}
		return map[string]any{"count": n}, nil, nil
	case "distinct":
		return runDistinct(ctx, coll, st)
	case "insertOne":
		if len(st.Args) < 1 {
			return nil, nil, errs.Config("MONGO_BAD_ARGS", "insertOne expects a document argument")
		}
		if _, err := coll.InsertOne(ctx, st.Args[0]); err != nil {
			return nil, nil, wrapErr(err)
		}
		return map[string]any{"affected": 1}, nil, nil
	case "insertMany":
		if len(st.Args) < 1 {
			return nil, nil, errs.Config("MONGO_BAD_ARGS", "insertMany expects an array argument")
		}
		docs, ok := st.Args[0].(bson.A)
		if !ok {
			return nil, nil, errs.Config("MONGO_BAD_ARGS", "insertMany expects an array")
		}
		r, err := coll.InsertMany(ctx, []any(docs))
		if err != nil {
			return nil, nil, wrapErr(err)
		}
		return map[string]any{"affected": len(r.InsertedIDs)}, nil, nil
	case "updateOne":
		return runUpdate(ctx, coll, st, false)
	case "updateMany":
		return runUpdate(ctx, coll, st, true)
	case "replaceOne":
		if len(st.Args) < 2 {
			return nil, nil, errs.Config("MONGO_BAD_ARGS", "replaceOne expects (filter, replacement)")
		}
		r, err := coll.ReplaceOne(ctx, filterArg(st, 0), st.Args[1])
		if err != nil {
			return nil, nil, wrapErr(err)
		}
		return map[string]any{"affected": r.ModifiedCount}, nil, nil
	case "deleteOne":
		r, err := coll.DeleteOne(ctx, filterArg(st, 0))
		if err != nil {
			return nil, nil, wrapErr(err)
		}
		return map[string]any{"affected": r.DeletedCount}, nil, nil
	case "deleteMany":
		r, err := coll.DeleteMany(ctx, filterArg(st, 0))
		if err != nil {
			return nil, nil, wrapErr(err)
		}
		return map[string]any{"affected": r.DeletedCount}, nil, nil
	case "findOneAndUpdate", "findOneAndReplace", "findOneAndDelete":
		return runFindAndModify(ctx, coll, st)
	}
	return nil, nil, errs.Config("MONGO_METHOD_REFUSED", "method "+st.Method+" is not supported")
}

func runFind(ctx context.Context, coll *mongo.Collection, st *Statement) (any, []string, error) {
	limit, err := findLimit(st)
	if err != nil {
		return nil, nil, err
	}
	opts := options.Find().SetLimit(int64(limit))
	if st.Method == "findOne" {
		opts.SetLimit(1)
	}
	if len(st.Args) >= 2 { // find(filter, projection)
		opts.SetProjection(st.Args[1])
	}
	for _, m := range st.Modifiers {
		switch m.Name {
		case "limit": // already handled by findLimit
		case "sort":
			opts.SetSort(m.Arg)
		case "skip":
			opts.SetSkip(asInt64(m.Arg))
		case "projection":
			opts.SetProjection(m.Arg)
		case "hint":
			opts.SetHint(m.Arg)
		case "toArray", "pretty":
			// no-op
		default:
			return nil, nil, errs.Config("MONGO_BAD_MODIFIER", "unsupported modifier ."+m.Name+"()")
		}
	}
	cur, err := coll.Find(ctx, filterArg(st, 0), opts)
	if err != nil {
		return nil, nil, wrapErr(err)
	}
	defer cur.Close(ctx)
	docs := []any{}
	for cur.Next(ctx) {
		var d bson.M
		if err := cur.Decode(&d); err != nil {
			return nil, nil, wrapErr(err)
		}
		docs = append(docs, coerce(d))
	}
	if err := cur.Err(); err != nil {
		return nil, nil, wrapErr(err)
	}
	if st.Method == "findOne" {
		if len(docs) == 0 {
			return nil, nil, nil // {"data":null}
		}
		return docs[0], nil, nil
	}
	return docs, nil, nil
}

func runAggregate(ctx context.Context, coll *mongo.Collection, st *Statement) (any, []string, error) {
	pipeline := bson.A{}
	if len(st.Args) >= 1 {
		if a, ok := st.Args[0].(bson.A); ok {
			pipeline = a
		} else {
			return nil, nil, errs.Config("MONGO_BAD_ARGS", "aggregate expects a pipeline array")
		}
	}
	cur, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, nil, wrapErr(err)
	}
	defer cur.Close(ctx)
	docs := []any{}
	var warnings []string
	for cur.Next(ctx) {
		if len(docs) >= defaultDocCap {
			warnings = append(warnings, "result capped at 100 documents; add a $limit stage or paginate with $skip")
			break
		}
		var d bson.M
		if err := cur.Decode(&d); err != nil {
			return nil, nil, wrapErr(err)
		}
		docs = append(docs, coerce(d))
	}
	if err := cur.Err(); err != nil {
		return nil, nil, wrapErr(err)
	}
	return docs, warnings, nil
}

func runDistinct(ctx context.Context, coll *mongo.Collection, st *Statement) (any, []string, error) {
	if len(st.Args) < 1 {
		return nil, nil, errs.Config("MONGO_BAD_ARGS", "distinct expects a field name")
	}
	field, ok := st.Args[0].(string)
	if !ok {
		return nil, nil, errs.Config("MONGO_BAD_ARGS", "distinct field must be a string")
	}
	vals, err := coll.Distinct(ctx, field, filterArg(st, 1))
	if err != nil {
		return nil, nil, wrapErr(err)
	}
	var warnings []string
	if len(vals) > defaultDocCap {
		vals = vals[:defaultDocCap]
		warnings = append(warnings, "result capped at 100 elements; narrow the filter to reduce distinct values")
	}
	return coerceSlice(vals), warnings, nil
}

func runUpdate(ctx context.Context, coll *mongo.Collection, st *Statement, many bool) (any, []string, error) {
	if len(st.Args) < 2 {
		return nil, nil, errs.Config("MONGO_BAD_ARGS", st.Method+" expects (filter, update)")
	}
	var r *mongo.UpdateResult
	var err error
	if many {
		r, err = coll.UpdateMany(ctx, st.Args[0], st.Args[1])
	} else {
		r, err = coll.UpdateOne(ctx, st.Args[0], st.Args[1])
	}
	if err != nil {
		return nil, nil, wrapErr(err)
	}
	return map[string]any{"affected": r.ModifiedCount, "matched": r.MatchedCount}, nil, nil
}

func runFindAndModify(ctx context.Context, coll *mongo.Collection, st *Statement) (any, []string, error) {
	var res *mongo.SingleResult
	switch st.Method {
	case "findOneAndUpdate":
		if len(st.Args) < 2 {
			return nil, nil, errs.Config("MONGO_BAD_ARGS", "findOneAndUpdate expects (filter, update)")
		}
		res = coll.FindOneAndUpdate(ctx, filterArg(st, 0), st.Args[1])
	case "findOneAndReplace":
		if len(st.Args) < 2 {
			return nil, nil, errs.Config("MONGO_BAD_ARGS", "findOneAndReplace expects (filter, replacement)")
		}
		res = coll.FindOneAndReplace(ctx, filterArg(st, 0), st.Args[1])
	case "findOneAndDelete":
		res = coll.FindOneAndDelete(ctx, filterArg(st, 0))
	}
	var d bson.M
	if err := res.Decode(&d); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil, nil
		}
		return nil, nil, wrapErr(err)
	}
	return coerce(d), nil, nil
}

// findLimit resolves the effective document cap from a .limit() modifier.
func findLimit(st *Statement) (int, error) {
	for _, m := range st.Modifiers {
		if m.Name == "limit" {
			n := int(asInt64(m.Arg))
			if n > maxDocCap {
				return 0, errs.Config("LIMIT_TOO_LARGE",
					"limit exceeds the maximum 100; paginate with .skip().limit() or aggregate")
			}
			if n <= 0 { // 0 or unparseable → default cap, never mongo's "no limit"
				n = defaultDocCap
			}
			return n, nil
		}
	}
	return defaultDocCap, nil
}

// filterArg returns args[i] as a filter, defaulting to an empty document.
func filterArg(st *Statement, i int) any {
	if len(st.Args) > i && st.Args[i] != nil {
		return st.Args[i]
	}
	return bson.D{}
}

func asInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int32:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}

func wrapErr(err error) error {
	if err == nil {
		return nil
	}
	return errs.Remote("MONGO_QUERY", err.Error())
}
