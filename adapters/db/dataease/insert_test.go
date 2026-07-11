package dataease

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/no-today/aidev-clis/internal/dbcli"
)

func TestParseInsertArgs(t *testing.T) {
	cases := []struct {
		args        []string
		wantTable   string
		wantSQLHas  string
		wantExclude []string
	}{
		{[]string{"insert", "select", "*", "from", "t"}, "", "select * from t", nil},
		{[]string{"insert", "--table", "dst", "select", "1"}, "dst", "select 1", nil},
		{[]string{"insert", "--table=dst", "select", "1"}, "dst", "select 1", nil},
		{[]string{"insert", "--exclude", "ID, Created", "select", "1"}, "", "select 1", []string{"id", "created"}},
		{[]string{"insert", "--exclude=a", "--exclude=b,c", "select", "1"}, "", "select 1", []string{"a", "b", "c"}},
	}
	for _, c := range cases {
		table, exclude, sqlText := parseInsertArgs(c.args)
		if table != c.wantTable {
			t.Errorf("args %v: table = %q, want %q", c.args, table, c.wantTable)
		}
		if sqlText != c.wantSQLHas {
			t.Errorf("args %v: sql = %q, want %q", c.args, sqlText, c.wantSQLHas)
		}
		for _, e := range c.wantExclude {
			if !exclude[e] {
				t.Errorf("args %v: exclude missing %q (got %v)", c.args, e, exclude)
			}
		}
		if len(exclude) != len(c.wantExclude) {
			t.Errorf("args %v: exclude size = %d, want %d (%v)", c.args, len(exclude), len(c.wantExclude), exclude)
		}
	}
}

func TestInferTable(t *testing.T) {
	got, err := inferTable("select * from med2.olt_drug_provider where DP_ID in (1,2)")
	if err != nil {
		t.Fatal(err)
	}
	if got != "med2.olt_drug_provider" {
		t.Errorf("table = %q", got)
	}

	if _, err := inferTable("select 1"); err == nil {
		t.Error("expected INSERT_NO_TABLE for FROM-less query")
	}
	if _, err := inferTable("select * from a join b on a.id=b.id"); err == nil {
		t.Error("expected INSERT_NO_TABLE for joined query")
	}
}

func TestRenderValue(t *testing.T) {
	cases := map[any]string{
		nil:          "NULL",
		"plain":      "'plain'",
		"O'Brien":    "'O''Brien'",
		`back\slash`: `'back\\slash'`,
		`{"k":"v"}`:  `'{"k":"v"}'`,
	}
	for in, want := range cases {
		if got := renderValue(in); got != want {
			t.Errorf("renderValue(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderInsert_QuotesIdentifiersAndValues(t *testing.T) {
	var rc rawCapOut
	res := &dbcli.Result{Columns: []string{"id", "name"}, Rows: [][]any{{"1", "a'b"}, {"2", nil}}}
	if err := renderInsert(res, "med2.t", nil, &rc); err != nil {
		t.Fatal(err)
	}
	want := "INSERT INTO `med2`.`t` (`id`, `name`) VALUES ('1', 'a''b');\n" +
		"INSERT INTO `med2`.`t` (`id`, `name`) VALUES ('2', NULL);\n"
	if rc.text != want {
		t.Errorf("got:\n%s\nwant:\n%s", rc.text, want)
	}
}

func TestRenderInsert_ExcludesColumns(t *testing.T) {
	var rc rawCapOut
	res := &dbcli.Result{Columns: []string{"id", "name", "created"}, Rows: [][]any{{"1", "a", "t0"}}}
	// case-insensitive match on "Created"
	exclude := map[string]bool{"created": true}
	if err := renderInsert(res, "t", exclude, &rc); err != nil {
		t.Fatal(err)
	}
	want := "INSERT INTO `t` (`id`, `name`) VALUES ('1', 'a');\n"
	if rc.text != want {
		t.Errorf("got %q, want %q", rc.text, want)
	}
}

func TestRenderInsert_AllExcludedErrors(t *testing.T) {
	var rc rawCapOut
	res := &dbcli.Result{Columns: []string{"id"}, Rows: [][]any{{"1"}}}
	err := renderInsert(res, "t", map[string]bool{"id": true}, &rc)
	requireCode(t, err, "INSERT_NO_COLUMNS")
}

func TestRunInsert_ExcludeEndToEnd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"fields":[{"fieldName":"DP_ID"},{"fieldName":"DP_NAME"},{"fieldName":"CREATE_TIME"}],"data":[{"DP_ID":"1","DP_NAME":"店A","CREATE_TIME":"2025-01-01"}]}}`))
	}))
	defer srv.Close()
	if _, err := SaveSession(&Session{Token: "jwt", BaseURL: srv.URL}, "dataease.local.session", filepath.Join(home, "sessions")); err != nil {
		t.Fatal(err)
	}

	in := dbcliInput(srv.URL, "insert", "--table", "med2.t", "--exclude", "create_time", "select * from x")
	var rc rawCapOut
	if err := (adapter{}).Run(context.Background(), in, &rc); err != nil {
		t.Fatal(err)
	}
	want := "INSERT INTO `med2`.`t` (`DP_ID`, `DP_NAME`) VALUES ('1', '店A');\n"
	if rc.text != want {
		t.Errorf("got %q, want %q", rc.text, want)
	}
}

func TestRunInsert_EndToEnd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"fields":[{"fieldName":"DP_ID"},{"fieldName":"DP_NAME"}],"data":[{"DP_ID":"130000005","DP_NAME":"店A"}]}}`))
	}))
	defer srv.Close()
	if _, err := SaveSession(&Session{Token: "jwt", BaseURL: srv.URL}, "dataease.local.session", filepath.Join(home, "sessions")); err != nil {
		t.Fatal(err)
	}

	in := dbcliInput(srv.URL, "insert", "select", "*", "from", "med2.olt_drug_provider", "where", "DP_ID=130000005")
	var rc rawCapOut
	if err := (adapter{}).Run(context.Background(), in, &rc); err != nil {
		t.Fatal(err)
	}
	want := "INSERT INTO `med2`.`olt_drug_provider` (`DP_ID`, `DP_NAME`) VALUES ('130000005', '店A');\n"
	if rc.text != want {
		t.Errorf("got %q, want %q", rc.text, want)
	}
}

func TestRunInsert_RejectsNonSelect(t *testing.T) {
	cases := []struct {
		name string
		args []string
		code string
	}{
		{"update", []string{"insert", "update", "t", "set", "x=1"}, "WRITE_NOT_ALLOWED"},
		{"cte-delete", []string{"insert", "with x as (select id from t) delete from t where id in (select id from x)"}, "WRITE_NOT_ALLOWED"},
		{"multi-statement", []string{"insert", "select 1; drop table t"}, "MULTI_STATEMENT"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// No AIDEV_CLIS_HOME session and a bogus URL: the gate must reject
			// before any session load or HTTP call.
			t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
			in := dbcliInput("https://example.test/dataease", c.args...)
			var rc rawCapOut
			err := (adapter{}).Run(context.Background(), in, &rc)
			requireCode(t, err, c.code)
		})
	}
}
