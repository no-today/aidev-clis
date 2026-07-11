package mongo

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/no-today/aidev-clis/internal/dbcli"
)

func listDatabases(ctx context.Context, client *mongo.Client, out dbcli.Output) error {
	names, err := client.ListDatabaseNames(ctx, bson.D{})
	if err != nil {
		return wrapErr(err)
	}
	rows := make([][]any, 0, len(names))
	for _, n := range names {
		rows = append(rows, []any{n})
	}
	return out.Batch(dbcli.Result{Columns: []string{"database"}, Rows: rows})
}

func listCollections(ctx context.Context, db *mongo.Database, out dbcli.Output) error {
	names, err := db.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return wrapErr(err)
	}
	rows := make([][]any, 0, len(names))
	for _, n := range names {
		rows = append(rows, []any{db.Name(), n})
	}
	return out.Batch(dbcli.Result{Columns: []string{"database", "name"}, Rows: rows})
}

// describeCollection reports a collection's document count + indexes (mongo is
// schemaless, so there are no fixed columns).
func describeCollection(ctx context.Context, db *mongo.Database, name string, out dbcli.Output) error {
	coll := db.Collection(name)
	count, err := coll.EstimatedDocumentCount(ctx)
	if err != nil {
		return wrapErr(err)
	}
	cur, err := coll.Indexes().List(ctx)
	if err != nil {
		return wrapErr(err)
	}
	defer cur.Close(ctx)
	var indexes []any
	for cur.Next(ctx) {
		var d bson.M
		if err := cur.Decode(&d); err != nil {
			return wrapErr(err)
		}
		indexes = append(indexes, coerce(d))
	}
	return out.Batch(map[string]any{
		"database":   db.Name(),
		"collection": name,
		"count":      count,
		"indexes":    indexes,
	})
}
