package dbcli

import (
	"bytes"
	"testing"
)

func TestRenderRaw_ResultIsTSVNoHeader(t *testing.T) {
	var b bytes.Buffer
	res := Result{Columns: []string{"id", "name"}, Rows: [][]any{{int64(1), "alice"}, {int64(2), nil}}}
	if err := renderRaw(&b, res); err != nil {
		t.Fatal(err)
	}
	want := "1\talice\n2\t\n"
	if b.String() != want {
		t.Fatalf("Result raw = %q, want %q", b.String(), want)
	}
}

func TestRenderRaw_MapIsKeyValueSorted(t *testing.T) {
	var b bytes.Buffer
	if err := renderRaw(&b, map[string]any{"ok": true, "affected": 3}); err != nil {
		t.Fatal(err)
	}
	want := "affected=3\nok=true\n"
	if b.String() != want {
		t.Fatalf("map raw = %q, want %q", b.String(), want)
	}
}

func TestRenderRaw_PointerResult(t *testing.T) {
	var b bytes.Buffer
	res := &Result{Columns: []string{"x"}, Rows: [][]any{{"v"}}}
	if err := renderRaw(&b, res); err != nil {
		t.Fatal(err)
	}
	if b.String() != "v\n" {
		t.Fatalf("*Result raw = %q, want %q", b.String(), "v\n")
	}
}

func TestRenderRaw_StructViaJSONRoundTrip(t *testing.T) {
	type col struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	type schema struct {
		Database string `json:"database"`
		Table    string `json:"table"`
		Columns  []col  `json:"columns"`
	}
	var b bytes.Buffer
	s := schema{Database: "app", Table: "users", Columns: []col{{Name: "id", Type: "int"}}}
	if err := renderRaw(&b, s); err != nil {
		t.Fatal(err)
	}
	want := "columns=[{\"name\":\"id\",\"type\":\"int\"}]\ndatabase=app\ntable=users\n"
	if b.String() != want {
		t.Fatalf("struct raw = %q, want %q", b.String(), want)
	}
}
