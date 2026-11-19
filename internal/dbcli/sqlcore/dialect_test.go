package sqlcore

import (
	"context"
	"database/sql"
	"testing"
)

// stubDialect proves the Dialect interface is implementable.
type stubDialect struct{}

func (stubDialect) DriverName() string                                       { return "stub" }
func (stubDialect) SystemNamespaces() []string                               { return nil }
func (stubDialect) ListDatabases(context.Context, *sql.DB) ([]string, error) { return nil, nil }
func (stubDialect) ListTables(context.Context, *sql.DB, string, string) ([]Table, error) {
	return nil, nil
}
func (stubDialect) Describe(context.Context, *sql.DB, string, string) (TableSchema, error) {
	return TableSchema{}, nil
}
func (stubDialect) UseDatabase(context.Context, *sql.Conn, string) error { return nil }
func (stubDialect) BackendID(context.Context, *sql.Conn) (string, error) { return "", nil }
func (stubDialect) CancelQuery(context.Context, *sql.DB, string) error   { return nil }

func TestDialectIsImplementable(t *testing.T) {
	var _ Dialect = stubDialect{}
	tbl := Table{Database: "app", Name: "orders"}
	if tbl.Database != "app" || tbl.Name != "orders" {
		t.Fatal("Table fields")
	}
}
