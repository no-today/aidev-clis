package logcli

import (
	"bytes"
	"encoding/json"
	"testing"
)

func produce3(emit func(string) error) error {
	for _, l := range []string{"a", "b", "c"} {
		if err := emit(l); err != nil {
			return err
		}
	}
	return nil
}

func TestStreamLines_BatchWhenNoFollow(t *testing.T) {
	var b bytes.Buffer
	o := &runOutput{w: &b}
	if err := StreamLines(o, []string{"logs"}, produce3); err != nil {
		t.Fatal(err)
	}
	var env struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(b.Bytes(), &env); err != nil {
		t.Fatalf("not batch: %s", b.String())
	}
	if len(env.Data) != 3 || env.Data[0]["message"] != "a" {
		t.Fatalf("bad batch: %v", env.Data)
	}
}

func TestStreamLines_StreamWhenFollow(t *testing.T) {
	var b bytes.Buffer
	o := &runOutput{w: &b}
	if err := StreamLines(o, []string{"logs", "-f"}, produce3); err != nil {
		t.Fatal(err)
	}
	o.finalize(nil)
	lines := bytes.Count(bytes.TrimSpace(b.Bytes()), []byte("\n")) + 1
	if lines != 4 {
		t.Fatalf("want 4 lines, got %d: %s", lines, b.String())
	}
}
