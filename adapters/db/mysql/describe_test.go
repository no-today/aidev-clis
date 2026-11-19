package mysql

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestDescribe_ResolvesAndReturnsColumns(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	// resolve: table 'orders' is in exactly one db, with table charset/collation
	mock.ExpectQuery("information_schema.tables").
		WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_comment", "table_collation", "character_set_name"}).
			AddRow("app", "orders tbl", "utf8mb4_general_ci", "utf8mb4"))
	mock.ExpectQuery("information_schema.columns").
		WillReturnRows(sqlmock.NewRows([]string{"column_name", "column_type", "is_nullable", "column_key", "column_default", "column_comment", "character_set_name", "collation_name", "extra"}).
			AddRow("id", "bigint", "NO", "PRI", nil, "primary key", nil, nil, "auto_increment").
			AddRow("name", "varchar(64)", "YES", "", nil, "display name", "utf8mb4", "utf8mb4_general_ci", ""))
	mock.ExpectQuery("information_schema.statistics").
		WillReturnRows(sqlmock.NewRows([]string{"index_name", "column_name", "non_unique"}).
			AddRow("PRIMARY", "id", int64(0)))

	ts, err := dialect{}.Describe(context.Background(), db, "", "orders")
	if err != nil {
		t.Fatal(err)
	}
	// sqlcore.Column.Key is a string (e.g. "PRI"); Index.Unique is a bool.
	if ts.Database != "app" || len(ts.Columns) != 2 || ts.Columns[0].Name != "id" || ts.Columns[0].Key != "PRI" {
		t.Fatalf("describe: %+v", ts)
	}
	if ts.Charset != "utf8mb4" || ts.Collation != "utf8mb4_general_ci" {
		t.Fatalf("table charset/collation: %+v", ts)
	}
	if ts.Columns[0].Comment != "primary key" || ts.Columns[0].Extra != "auto_increment" {
		t.Fatalf("col[0] meta: %+v", ts.Columns[0])
	}
	if ts.Columns[1].Charset != "utf8mb4" || ts.Columns[1].Collation != "utf8mb4_general_ci" || ts.Columns[1].Comment != "display name" {
		t.Fatalf("col[1] meta: %+v", ts.Columns[1])
	}
	if len(ts.Indexes) != 1 || !ts.Indexes[0].Unique {
		t.Fatalf("indexes: %+v", ts.Indexes)
	}
}

func TestDescribe_Ambiguous(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("information_schema.tables").
		WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_comment", "table_collation", "character_set_name"}).
			AddRow("app", "", "", "").AddRow("billing", "", "", ""))
	_, err := dialect{}.Describe(context.Background(), db, "", "orders")
	if err == nil || !strings.Contains(err.Error(), "TABLE_AMBIGUOUS") {
		t.Fatalf("want TABLE_AMBIGUOUS, got %v", err)
	}
}
