package sqlcore

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestQueryRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(int64(1), []byte("alice")).
		AddRow(int64(2), nil)
	mock.ExpectQuery("SELECT").WillReturnRows(rows)

	res, err := queryRows(context.Background(), db, "SELECT id,name FROM t")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Columns) != 2 || res.Columns[0] != "id" {
		t.Fatalf("columns: %v", res.Columns)
	}
	if len(res.Rows) != 2 || res.Rows[0][1] != "alice" { // []byte coerced to string
		t.Fatalf("rows: %v", res.Rows)
	}
	if res.Rows[1][1] != nil {
		t.Fatalf("NULL should be nil, got %v", res.Rows[1][1])
	}
}

func TestExecStmt(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectExec("UPDATE").WillReturnResult(sqlmock.NewResult(0, 3))

	n, err := execStmt(context.Background(), db, "UPDATE t SET a=1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("affected = %d, want 3", n)
	}
}
