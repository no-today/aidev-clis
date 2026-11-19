package sqlcore

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/dbcli"
)

// capOut captures the Batch payload + warnings.
type capOut struct {
	payload  any
	warnings []string
}

func (c *capOut) Batch(p any, w ...string) error { c.payload = p; c.warnings = w; return nil }

func runWith(t *testing.T, in dbcli.Input, setup func(sqlmock.Sqlmock)) (*capOut, error) {
	t.Helper()
	db, mock, _ := sqlmock.New()
	defer db.Close()
	setup(mock)
	out := &capOut{}
	err := Run(context.Background(), stubDialect{}, db, in, out)
	return out, err
}

func TestRun_ReadAppliesAutoLimitAndEmits(t *testing.T) {
	out, err := runWith(t, dbcli.Input{Args: []string{"SELECT id FROM t"}}, func(m sqlmock.Sqlmock) {
		// the executed query must carry the appended LIMIT 100
		m.ExpectQuery("SELECT id FROM t LIMIT 100").
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))
	})
	if err != nil {
		t.Fatal(err)
	}
	res := out.payload.(dbcli.Result)
	if len(res.Rows) != 1 {
		t.Fatalf("rows: %v", res.Rows)
	}
}

func TestRun_WriteNeedsAllowWrite(t *testing.T) {
	_, err := runWith(t, dbcli.Input{Args: []string{"DELETE FROM t"}}, func(m sqlmock.Sqlmock) {})
	e, ok := err.(*errs.Error)
	if !ok || e.Code != "WRITE_NOT_ALLOWED" {
		t.Fatalf("want WRITE_NOT_ALLOWED, got %v", err)
	}
}

func TestRun_WriteWithAllowWriteReturnsAffected(t *testing.T) {
	out, err := runWith(t, dbcli.Input{Args: []string{"DELETE FROM t"}, AllowWrite: true}, func(m sqlmock.Sqlmock) {
		m.ExpectExec("DELETE FROM t").WillReturnResult(sqlmock.NewResult(0, 4))
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.payload.(map[string]any)["affected"] != int64(4) {
		t.Fatalf("affected payload: %v", out.payload)
	}
}

func TestRun_DDLAlwaysRefused(t *testing.T) {
	_, err := runWith(t, dbcli.Input{Args: []string{"DROP TABLE t"}, AllowWrite: true}, func(m sqlmock.Sqlmock) {})
	e, ok := err.(*errs.Error)
	if !ok || e.Code != "DDL_REFUSED" {
		t.Fatalf("want DDL_REFUSED, got %v", err)
	}
}

func TestRun_MultiStatementRejected(t *testing.T) {
	_, err := runWith(t, dbcli.Input{Args: []string{"SELECT 1; DROP TABLE t"}}, func(m sqlmock.Sqlmock) {})
	e, ok := err.(*errs.Error)
	if !ok || e.Code != "MULTI_STATEMENT" {
		t.Fatalf("want MULTI_STATEMENT, got %v", err)
	}
}

func TestRun_ExplicitLimitHonored(t *testing.T) {
	// an explicit LIMIT ≤ 100 runs verbatim (no clamp, no warning)
	out, err := runWith(t, dbcli.Input{Args: []string{"SELECT id FROM t LIMIT 50"}}, func(m sqlmock.Sqlmock) {
		m.ExpectQuery("SELECT id FROM t LIMIT 50").
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.warnings) != 0 {
		t.Fatalf("honored LIMIT must not warn, got %v", out.warnings)
	}
}

func TestRun_LimitTooLargeRejected(t *testing.T) {
	// LIMIT > 100 errors before running (no ExpectQuery).
	_, err := runWith(t, dbcli.Input{Args: []string{"SELECT id FROM t LIMIT 5000"}}, func(m sqlmock.Sqlmock) {})
	e, ok := err.(*errs.Error)
	if !ok || e.Code != "LIMIT_TOO_LARGE" {
		t.Fatalf("want LIMIT_TOO_LARGE, got %v", err)
	}
}
