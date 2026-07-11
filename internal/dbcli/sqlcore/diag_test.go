package sqlcore

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/no-today/aidev-clis/internal/core/diag"
	"github.com/no-today/aidev-clis/internal/dbcli"
)

func TestRun_Verbose_ReadEmitsFinalSQLRowsAndTiming(t *testing.T) {
	d := diag.New(2)
	ctx := diag.With(context.Background(), d)

	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("SELECT id FROM t LIMIT 100").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)).AddRow(int64(2)))

	out := &capOut{}
	if err := Run(ctx, stubDialect{}, db, dbcli.Input{Args: []string{"SELECT id FROM t"}}, out); err != nil {
		t.Fatal(err)
	}

	lines := strings.Join(d.Lines(), "\n")
	if !strings.Contains(lines, "SELECT id FROM t LIMIT 100") {
		t.Fatalf("final SQL (with auto-LIMIT) must appear at -vv: %q", lines)
	}
	if !strings.Contains(lines, "2 row") {
		t.Fatalf("returned row count must appear: %q", lines)
	}
	if !strings.Contains(lines, "connected in") {
		t.Fatalf("connect timing must appear: %q", lines)
	}
	if !strings.Contains(lines, "query") {
		t.Fatalf("query timing must appear: %q", lines)
	}
}

func TestRun_Verbose_WriteEmitsAffectedRows(t *testing.T) {
	d := diag.New(1)
	ctx := diag.With(context.Background(), d)

	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectExec("UPDATE t SET x=1").WillReturnResult(sqlmock.NewResult(0, 3))

	out := &capOut{}
	if err := Run(ctx, stubDialect{}, db, dbcli.Input{Args: []string{"UPDATE t SET x=1"}, AllowWrite: true}, out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Join(d.Lines(), "\n")
	if !strings.Contains(lines, "3 row") {
		t.Fatalf("affected row count must appear: %q", lines)
	}
}

func TestRun_LevelOne_OmitsFinalSQL(t *testing.T) {
	d := diag.New(1) // -v: high-level only, no SQL text
	ctx := diag.With(context.Background(), d)

	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("SELECT id FROM t LIMIT 100").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))

	out := &capOut{}
	if err := Run(ctx, stubDialect{}, db, dbcli.Input{Args: []string{"SELECT id FROM t"}}, out); err != nil {
		t.Fatal(err)
	}
	if lines := strings.Join(d.Lines(), "\n"); strings.Contains(lines, "LIMIT 100") {
		t.Fatalf("-v must not include final SQL text (that is -vv): %q", lines)
	}
}
