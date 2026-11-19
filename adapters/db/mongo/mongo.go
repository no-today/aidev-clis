// Package mongo is the dbcli "mongo" driver: a parsed mongosh-statement
// pass-through with a method guard (read / write-needs--allow-write / refused).
// Standalone (documents are nested JSON, not tabular).
package mongo

import (
	"context"
	"strings"

	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/dbcli"
)

type adapter struct{}

func New() dbcli.Driver { return adapter{} }

func (adapter) Name() string { return "mongo" }

func (a adapter) Run(ctx context.Context, in dbcli.Input, out dbcli.Output) error {
	if len(in.Args) == 0 {
		return errs.Config("EMPTY_STATEMENT", "no mongo statement provided")
	}
	client, dbName, cleanup, err := connect(ctx, in)
	if err != nil {
		return err
	}
	defer cleanup()
	db := client.Database(dbName)

	switch strings.ToLower(in.Args[0]) {
	case "databases":
		return listDatabases(ctx, client, out)
	case "tables":
		return listCollections(ctx, db, out)
	case "describe":
		if len(in.Args) < 2 {
			return errs.Config("DESCRIBE_NO_COLLECTION", "describe requires a collection")
		}
		return describeCollection(ctx, db, in.Args[1], out)
	case "doctor":
		return out.Batch(map[string]any{"ok": true})
	}

	st, err := ParseStatement(strings.Join(in.Args, " "))
	if err != nil {
		return err
	}
	class := Classify(st.Method)
	if class == ClassRefused {
		return errs.Config("MONGO_METHOD_REFUSED", "method "+st.Method+" is not allowed (DDL/admin)")
	}
	// Server-side JS ($where/$function/$accumulator) is an arbitrary-code + DoS
	// surface even in a "read"; refuse it outright (it's never needed for an
	// ad-hoc query and the read-only cred wouldn't constrain CPU abuse).
	for _, arg := range st.Args {
		if HasServerJS(arg) {
			return errs.Config("MONGO_JS_REFUSED", "server-side JavaScript ($where/$function/$accumulator) is not allowed")
		}
	}
	if st.Method == "aggregate" && len(st.Args) >= 1 && AggregateWrites(st.Args[0]) {
		class = ClassWrite
	}
	if class == ClassWrite && !in.AllowWrite {
		return errs.Config("WRITE_NOT_ALLOWED", "this statement writes; pass --allow-write to permit it")
	}

	payload, warnings, err := Execute(ctx, db.Collection(st.Collection), st)
	if err != nil {
		return err
	}
	if p, cut := capStrings(payload, cellCap); cut {
		payload = p
		warnings = append(warnings, "field(s) truncated to 256 chars")
	}
	return out.Batch(payload, warnings...)
}
