package sqlcore

import (
	"context"
	"database/sql"
)

// Table is one discovered table (database = the qualifier namespace).
type Table struct {
	Database string `json:"database"`
	Name     string `json:"name"`
}

// Column is one column in a table's schema.
type Column struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Nullable  bool   `json:"nullable"`
	Key       string `json:"key,omitempty"`
	Default   string `json:"default,omitempty"`
	Comment   string `json:"comment,omitempty"`
	Charset   string `json:"charset,omitempty"`   // mysql only; pg/sqlite have no per-column charset
	Collation string `json:"collation,omitempty"` // mysql + pg (text columns)
	Extra     string `json:"extra,omitempty"`     // auto_increment / on update ... ; pg: identity / generated
}

// Index is one index on a table.
type Index struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique"`
}

// TableSchema is the describe payload.
type TableSchema struct {
	Database  string   `json:"database"`
	Table     string   `json:"table"`
	Comment   string   `json:"comment,omitempty"`
	Charset   string   `json:"charset,omitempty"`   // mysql only
	Collation string   `json:"collation,omitempty"` // mysql only (pg encoding is db-level)
	Columns   []Column `json:"columns"`
	Indexes   []Index  `json:"indexes"`
}

// Dialect is the per-engine seam: catalog queries + connection-id/cancel. The
// generic Run loop (guard/auto-LIMIT/exec) is engine-independent and lives here.
type Dialect interface {
	DriverName() string         // database/sql driver name ("mysql"/"pgx")
	SystemNamespaces() []string // namespaces excluded from discovery

	ListDatabases(ctx context.Context, db *sql.DB) ([]string, error)
	ListTables(ctx context.Context, db *sql.DB, database, like string) ([]Table, error)
	Describe(ctx context.Context, db *sql.DB, database, table string) (TableSchema, error)
	UseDatabase(ctx context.Context, conn *sql.Conn, database string) error

	// BackendID returns the server-side id of conn (for a later cancel).
	BackendID(ctx context.Context, conn *sql.Conn) (string, error)
	// CancelQuery kills the query running under backend id, via a fresh conn.
	CancelQuery(ctx context.Context, db *sql.DB, backendID string) error
}
