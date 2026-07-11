package sqlcore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	coreerrs "github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/dbcli"
)

type stringer struct{ s string }

func (x stringer) String() string { return x.s }

func TestRenderValue_StandardFlavor(t *testing.T) {
	f := flavorFor("pgx")
	cases := []struct {
		in   any
		want string
	}{
		{nil, "NULL"},
		{int64(42), "42"},
		{int32(7), "7"},
		{float64(1.5), "1.5"},
		{true, "TRUE"},
		{false, "FALSE"},
		{"a'b", "'a''b'"},
		{`a\b`, `'a\b'`}, // standard: backslash is literal, NOT doubled
		{[]byte("hi"), "'hi'"},
		{[]byte{0x00, 0xff}, `'\x00ff'`},
		{time.Date(2026, 6, 29, 1, 2, 3, 0, time.UTC), "'2026-06-29T01:02:03Z'"},
		{stringer{"12.34"}, "'12.34'"},
	}
	for _, c := range cases {
		if got := f.renderValue(c.in); got != c.want {
			t.Errorf("renderValue(%#v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRenderValue_MySQLFlavor(t *testing.T) {
	f := flavorFor("mysql")
	cases := []struct {
		in   any
		want string
	}{
		{true, "1"},
		{false, "0"},
		{`a\b`, `'a\\b'`}, // mysql: backslash doubled
		{"a'b", "'a''b'"},
		{[]byte("alice"), "'alice'"},   // valid UTF-8 []byte -> quoted, NOT hex
		{[]byte{0xff, 0xfe}, "0xfffe"}, // 0xff is never valid UTF-8 -> hex blob
	}
	for _, c := range cases {
		if got := f.renderValue(c.in); got != c.want {
			t.Errorf("renderValue(%#v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestQuoteIdentAndTable(t *testing.T) {
	my := flavorFor("mysql")
	if got := my.quoteIdent("id"); got != "`id`" {
		t.Errorf("mysql quoteIdent = %q", got)
	}
	if got := my.quoteTable("app.users"); got != "`app`.`users`" {
		t.Errorf("mysql quoteTable = %q", got)
	}
	std := flavorFor("pgx")
	if got := std.quoteTable("public.users"); got != `"public"."users"` {
		t.Errorf("std quoteTable = %q", got)
	}
	// embedded quote char is doubled
	if got := my.quoteIdent("a`b"); got != "`a``b`" {
		t.Errorf("mysql quoteIdent escape = %q", got)
	}
	// quoted-in-source table token is unwrapped before re-quoting
	if got := my.quoteTable("`users`"); got != "`users`" {
		t.Errorf("mysql quoteTable unwrap = %q", got)
	}
}

func TestParseInsertArgs(t *testing.T) {
	tbl, sql := parseInsertArgs([]string{"insert", "SELECT id FROM users"})
	if tbl != "" || sql != "SELECT id FROM users" {
		t.Fatalf("got tbl=%q sql=%q", tbl, sql)
	}
	tbl, sql = parseInsertArgs([]string{"insert", "--table", "u", "SELECT 1 FROM a JOIN b"})
	if tbl != "u" || sql != "SELECT 1 FROM a JOIN b" {
		t.Fatalf("got tbl=%q sql=%q", tbl, sql)
	}
	tbl, _ = parseInsertArgs([]string{"insert", "--table=orders", "SELECT 1"})
	if tbl != "orders" {
		t.Fatalf("got tbl=%q", tbl)
	}
}

func TestInferTable(t *testing.T) {
	ok := map[string]string{
		"SELECT * FROM users":                      "users",
		"SELECT id FROM app.users WHERE id < 10":   "app.users",
		"select id from users u where u.id = 1":    "users",
		"SELECT id FROM users AS u":                "users",
		"SELECT id FROM users WHERE name='FROM x'": "users", // FROM inside literal ignored
	}
	for sql, want := range ok {
		got, err := inferTable(sql)
		if err != nil || got != want {
			t.Errorf("inferTable(%q) = %q, %v; want %q", sql, got, err, want)
		}
	}
	bad := []string{
		"SELECT 1",                            // no FROM
		"SELECT * FROM a JOIN b ON a.id=b.id", // join
		"SELECT * FROM a, b",                  // comma tables
		"SELECT * FROM (SELECT 1) t",          // subquery
	}
	for _, sql := range bad {
		if _, err := inferTable(sql); err == nil {
			t.Errorf("inferTable(%q) should error", sql)
		}
	}
}

// rawCap captures Raw output; Batch is an error (insert must use Raw).
type rawCap struct{ sb strings.Builder }

func (r *rawCap) Batch(any, ...string) error { return errors.New("Batch called for insert verb") }
func (r *rawCap) Raw(s string) error         { r.sb.WriteString(s); return nil }

// namedDialect overrides stubDialect.DriverName for flavor selection.
type namedDialect struct {
	stubDialect
	name string
}

func (d namedDialect) DriverName() string { return d.name }

func runInsertWith(t *testing.T, d Dialect, in dbcli.Input, setup func(sqlmock.Sqlmock)) (*rawCap, error) {
	t.Helper()
	db, mock, _ := sqlmock.New()
	defer db.Close()
	setup(mock)
	out := &rawCap{}
	err := Run(context.Background(), d, db, in, out)
	return out, err
}

func TestInsert_RendersRowsMySQLFlavor(t *testing.T) {
	out, err := runInsertWith(t, namedDialect{name: "mysql"},
		dbcli.Input{Args: []string{"insert", "SELECT id, name FROM users"}},
		func(m sqlmock.Sqlmock) {
			m.ExpectQuery("SELECT id, name FROM users LIMIT 100").
				WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).
					AddRow(int64(1), "alice").
					AddRow(int64(2), "o'brien"))
		})
	if err != nil {
		t.Fatal(err)
	}
	want := "INSERT INTO `users` (`id`, `name`) VALUES (1, 'alice');\n" +
		"INSERT INTO `users` (`id`, `name`) VALUES (2, 'o''brien');\n"
	if out.sb.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out.sb.String(), want)
	}
}

func TestInsert_StandardFlavorAndTableOverride(t *testing.T) {
	out, err := runInsertWith(t, namedDialect{name: "pgx"},
		dbcli.Input{Args: []string{"insert", "--table", "public.t", "SELECT id FROM v"}},
		func(m sqlmock.Sqlmock) {
			m.ExpectQuery("SELECT id FROM v LIMIT 100").
				WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(7)))
		})
	if err != nil {
		t.Fatal(err)
	}
	want := `INSERT INTO "public"."t" ("id") VALUES (7);` + "\n"
	if out.sb.String() != want {
		t.Fatalf("got %q want %q", out.sb.String(), want)
	}
}

func TestInsert_ZeroRowsEmitsNothing(t *testing.T) {
	out, err := runInsertWith(t, namedDialect{name: "mysql"},
		dbcli.Input{Args: []string{"insert", "SELECT id FROM users"}},
		func(m sqlmock.Sqlmock) {
			m.ExpectQuery("SELECT id FROM users LIMIT 100").
				WillReturnRows(sqlmock.NewRows([]string{"id"}))
		})
	if err != nil {
		t.Fatal(err)
	}
	if out.sb.String() != "" {
		t.Fatalf("want empty, got %q", out.sb.String())
	}
}

func TestInsert_WriteRejected(t *testing.T) {
	_, err := runInsertWith(t, namedDialect{name: "mysql"},
		dbcli.Input{Args: []string{"insert", "DELETE FROM users"}},
		func(m sqlmock.Sqlmock) {})
	var e *coreerrs.Error
	if !errors.As(err, &e) || e.Code != "WRITE_NOT_ALLOWED" {
		t.Fatalf("want WRITE_NOT_ALLOWED, got %v", err)
	}
}

func TestInsert_NoTableInferable(t *testing.T) {
	_, err := runInsertWith(t, namedDialect{name: "mysql"},
		dbcli.Input{Args: []string{"insert", "SELECT * FROM a JOIN b ON a.id=b.id"}},
		func(m sqlmock.Sqlmock) {})
	var e *coreerrs.Error
	if !errors.As(err, &e) || e.Code != "INSERT_NO_TABLE" {
		t.Fatalf("want INSERT_NO_TABLE, got %v", err)
	}
}
