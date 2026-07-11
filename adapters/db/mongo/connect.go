package mongo

import (
	"context"
	"strings"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/dbcli"
	"github.com/no-today/aidev-clis/internal/dbcli/dbconn"
)

// connect resolves the URI (password + optional tunnel via dbconn), opens the
// client, and returns the target database name + a cleanup.
func connect(ctx context.Context, in dbcli.Input) (*mongo.Client, string, func(), error) {
	u, cleanup, err := dbconn.Resolve(ctx, in.Target)
	if err != nil {
		return nil, "", nil, err
	}
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(u.String()))
	if err != nil {
		cleanup()
		return nil, "", nil, errs.Config("MONGO_CONNECT", dbconn.Redact(err.Error(), u))
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(ctx)
		cleanup()
		return nil, "", nil, errs.Remote("MONGO_CONNECT", dbconn.Redact(err.Error(), u))
	}
	dbName := in.Database
	if dbName == "" {
		dbName = strings.TrimPrefix(u.Path, "/")
	}
	if dbName == "" {
		dbName = "test"
	}
	return client, dbName, func() { _ = client.Disconnect(context.Background()); cleanup() }, nil
}
