package sqlcore

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/no-today/aidev-clis/internal/dbcli"
)

// useDialect records that UseDatabase ran.
type useDialect struct {
	stubDialect
	used string
}

func (u *useDialect) UseDatabase(_ context.Context, _ *sql.Conn, db string) error {
	u.used = db
	return nil
}

func TestRun_DatabaseScopeCallsUseDatabase(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("SELECT id FROM t LIMIT 100").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))

	d := &useDialect{}
	out := &capOut{}
	err := Run(context.Background(), d, db, dbcli.Input{Args: []string{"SELECT id FROM t"}, Database: "app"}, out)
	if err != nil {
		t.Fatal(err)
	}
	if d.used != "app" {
		t.Fatalf("UseDatabase should have been called with app, got %q", d.used)
	}
}
