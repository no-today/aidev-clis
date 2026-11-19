package pgwire

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestDescribe_ResolvesAndColumns(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("pg_tables").
		WillReturnRows(sqlmock.NewRows([]string{"schemaname"}).AddRow("public"))
	cols := []string{"column_name", "data_type", "is_nullable", "column_default",
		"character_maximum_length", "numeric_precision", "numeric_scale", "collation_name",
		"is_identity", "is_generated", "comment"}
	mock.ExpectQuery("information_schema.columns").
		WillReturnRows(sqlmock.NewRows(cols).
			AddRow("id", "bigint", "NO", nil, nil, int64(64), int64(0), nil, "YES", "NEVER", "the id").
			AddRow("name", "character varying", "YES", nil, int64(64), nil, nil, "en_US", "NO", "NEVER", "display name").
			AddRow("amount", "numeric", "YES", nil, nil, int64(10), int64(2), nil, "NO", "NEVER", nil))
	mock.ExpectQuery("pg_index").
		WillReturnRows(sqlmock.NewRows([]string{"index_name", "column_name", "is_unique"}).
			AddRow("users_pkey", "id", true))
	mock.ExpectQuery("obj_description").
		WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow("the users"))

	ts, err := Dialect{}.Describe(context.Background(), db, "", "users")
	if err != nil {
		t.Fatal(err)
	}
	if ts.Database != "public" || len(ts.Columns) != 3 || ts.Columns[0].Name != "id" || ts.Columns[0].Nullable {
		t.Fatalf("describe: %+v", ts)
	}
	// id: bigint stays bare (numeric_precision would be noise); identity → extra; comment surfaced.
	if ts.Columns[0].Type != "bigint" || ts.Columns[0].Extra != "identity" || ts.Columns[0].Comment != "the id" {
		t.Fatalf("col[0]: %+v", ts.Columns[0])
	}
	// name: char length reattached, collation surfaced.
	if ts.Columns[1].Type != "character varying(64)" || ts.Columns[1].Collation != "en_US" || ts.Columns[1].Comment != "display name" {
		t.Fatalf("col[1]: %+v", ts.Columns[1])
	}
	// amount: precision + scale reattached.
	if ts.Columns[2].Type != "numeric(10,2)" {
		t.Fatalf("col[2]: %+v", ts.Columns[2])
	}
	if len(ts.Indexes) != 1 || !ts.Indexes[0].Unique {
		t.Fatalf("indexes: %+v", ts.Indexes)
	}
}

func TestDescribe_Ambiguous(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("pg_tables").
		WillReturnRows(sqlmock.NewRows([]string{"schemaname"}).AddRow("public").AddRow("sales"))
	_, err := Dialect{}.Describe(context.Background(), db, "", "orders")
	if err == nil || !strings.Contains(err.Error(), "TABLE_AMBIGUOUS") {
		t.Fatalf("want TABLE_AMBIGUOUS, got %v", err)
	}
}
