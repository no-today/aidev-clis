package mongo

import "go.mongodb.org/mongo-driver/bson"

// Class is the security class of a mongo method.
type Class int

const (
	ClassRead    Class = iota // find/aggregate/count/... — allowed
	ClassWrite                // insert/update/delete/... — needs --allow-write
	ClassRefused              // drop/index/admin/unknown — never allowed
)

var (
	readMethods = map[string]bool{
		"find": true, "findOne": true, "aggregate": true, "countDocuments": true,
		"count": true, "estimatedDocumentCount": true, "distinct": true,
	}
	writeMethods = map[string]bool{
		"insertOne": true, "insertMany": true, "updateOne": true, "updateMany": true,
		"replaceOne": true, "deleteOne": true, "deleteMany": true,
		"findOneAndUpdate": true, "findOneAndReplace": true, "findOneAndDelete": true,
	}
)

// Classify returns the class of a method. Unknown methods are refused (a closed
// allowlist — drop/createIndex/renameCollection/bulkWrite all fall here).
func Classify(method string) Class {
	switch {
	case readMethods[method]:
		return ClassRead
	case writeMethods[method]:
		return ClassWrite
	default:
		return ClassRefused
	}
}

// AggregateWrites reports whether an aggregate pipeline has a $out/$merge stage
// (which writes), so the guard reclassifies it as a write. The check is
// recursive — $out/$merge nested inside $facet, $unionWith, $lookup sub-pipelines
// are all detected.
func AggregateWrites(pipeline any) bool { return hasOutMerge(pipeline) }

// HasServerJS reports whether a filter/pipeline anywhere contains a server-side
// JavaScript operator ($where/$function/$accumulator) — an arbitrary-code/DoS
// surface the guard refuses even in a "read".
func HasServerJS(v any) bool {
	switch x := v.(type) {
	case bson.D:
		for _, e := range x {
			if isJSKey(e.Key) || HasServerJS(e.Value) {
				return true
			}
		}
	case bson.A:
		for _, e := range x {
			if HasServerJS(e) {
				return true
			}
		}
	case bson.M:
		for k, val := range x {
			if isJSKey(k) || HasServerJS(val) {
				return true
			}
		}
	}
	return false
}

func isJSKey(k string) bool {
	return k == "$where" || k == "$function" || k == "$accumulator"
}

func hasOutMerge(v any) bool {
	switch x := v.(type) {
	case bson.D:
		for _, e := range x {
			if e.Key == "$out" || e.Key == "$merge" {
				return true
			}
			if hasOutMerge(e.Value) {
				return true
			}
		}
	case bson.A:
		for _, e := range x {
			if hasOutMerge(e) {
				return true
			}
		}
	case bson.M:
		for k, val := range x {
			if k == "$out" || k == "$merge" {
				return true
			}
			if hasOutMerge(val) {
				return true
			}
		}
	}
	return false
}
