package envelope

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestWriteData_NoWarnings(t *testing.T) {
	var b bytes.Buffer
	if err := WriteData(&b, []map[string]any{{"id": 1}}, nil, false); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["data"]; !ok {
		t.Fatalf("missing data key: %s", b.String())
	}
	if _, ok := got["warnings"]; ok {
		t.Fatal("warnings must be omitted when empty")
	}
	if _, ok := got["status"]; ok {
		t.Fatal("status must not appear")
	}
}

func TestWriteData_WithWarnings(t *testing.T) {
	var b bytes.Buffer
	_ = WriteData(&b, []int{}, []string{"auto-LIMIT truncated"}, false)
	if !strings.Contains(b.String(), "auto-LIMIT truncated") {
		t.Fatalf("warnings missing: %s", b.String())
	}
}

func TestWriteError(t *testing.T) {
	var b bytes.Buffer
	_ = WriteError(&b, "BAD", "nope", false)
	var got struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error.Code != "BAD" || got.Error.Message != "nope" {
		t.Fatalf("bad error envelope: %s", b.String())
	}
}

func TestStream_RecordsThenEnd(t *testing.T) {
	var b bytes.Buffer
	s := NewStream(&b)
	_ = s.Record(map[string]any{"line": "a"})
	_ = s.Record(map[string]any{"line": "b"})
	_ = s.End(2)
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 ndjson lines, got %d: %s", len(lines), b.String())
	}
	var last map[string]any
	_ = json.Unmarshal([]byte(lines[2]), &last)
	if last["type"] != "end" {
		t.Fatalf("last line not end: %s", lines[2])
	}
}

func TestNoHTMLEscaping(t *testing.T) {
	var b bytes.Buffer
	_ = WriteData(&b, map[string]any{"msg": "<div> a & b </div>"}, nil, false)
	if !strings.Contains(b.String(), "<div> a & b </div>") {
		t.Fatalf("HTML chars must stay literal, got: %s", b.String())
	}
}

func TestWriteRawError(t *testing.T) {
	var b bytes.Buffer
	if err := WriteRawError(&b, "DB_CONN", "connection refused"); err != nil {
		t.Fatal(err)
	}
	if b.String() != "ERROR DB_CONN: connection refused\n" {
		t.Fatalf("raw error = %q", b.String())
	}
}
