package mysql

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestToMySQLDSN(t *testing.T) {
	u, _ := url.Parse("mysql://app_ro:p%40ss@10.0.0.1:3306/orders")
	dsn := toMySQLDSN(u)
	// go-sql-driver form: user:pass@tcp(host:port)/db?...
	for _, want := range []string{"app_ro:p@ss@tcp(10.0.0.1:3306)/orders", "parseTime=true"} {
		if !strings.Contains(dsn, want) {
			t.Fatalf("dsn %q missing %q", dsn, want)
		}
	}
}

func TestListDatabases_FiltersSystem(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("information_schema.schemata").
		WillReturnRows(sqlmock.NewRows([]string{"schema_name"}).AddRow("app").AddRow("billing"))
	got, err := dialect{}.ListDatabases(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "app" {
		t.Fatalf("databases: %v", got)
	}
}
