package pgwire

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestSystemNamespaces_KingbaseExtras(t *testing.T) {
	base := Dialect{}.SystemNamespaces()
	if !contains(base, "pg_catalog") || contains(base, "sys") {
		t.Fatalf("base pg system namespaces wrong: %v", base)
	}
	kb := Dialect{ExtraSystemSchemas: []string{"sys", "sysaudit", "oracle"}}.SystemNamespaces()
	if !contains(kb, "sys") || !contains(kb, "pg_catalog") {
		t.Fatalf("kingbase must add sys/... : %v", kb)
	}
}

func TestListDatabases_ExcludesSystem(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("information_schema.schemata").
		WillReturnRows(sqlmock.NewRows([]string{"schema_name"}).AddRow("public").AddRow("sales"))
	got, err := Dialect{}.ListDatabases(context.Background(), db)
	if err != nil || len(got) != 2 || got[0] != "public" {
		t.Fatalf("ListDatabases: %v %v", got, err)
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
