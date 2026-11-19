package aidev

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// auditLines reads every audit day-file under home and returns the parsed lines.
func auditLines(t *testing.T, home string) []map[string]any {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(home, "audit", "*.jsonl"))
	var out []map[string]any
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if l == "" {
				continue
			}
			var m map[string]any
			if err := json.Unmarshal([]byte(l), &m); err != nil {
				t.Fatalf("audit line not JSON: %v (%q)", err, l)
			}
			out = append(out, m)
		}
	}
	return out
}

// A successful discover writes exactly one terminal audit line: outcome "ok",
// no id (read-only, not two-phase), command naming the tool.
func TestRunAuditsSuccessOnce(t *testing.T) {
	home := writeHome(t)
	t.Setenv("AIDEV_SCENE", "companyA")
	var buf bytes.Buffer
	if code := Run(&buf, t.TempDir(), "json", false); code != 0 {
		t.Fatalf("code = %d, out = %s", code, buf.String())
	}
	ls := auditLines(t, home)
	if len(ls) != 1 {
		t.Fatalf("want exactly 1 audit line, got %d: %+v", len(ls), ls)
	}
	if ls[0]["outcome"] != "ok" {
		t.Errorf("outcome = %v, want ok", ls[0]["outcome"])
	}
	if _, hasID := ls[0]["id"]; hasID {
		t.Errorf("read-only op must not carry an id: %+v", ls[0])
	}
	cmd, _ := ls[0]["command"].(string)
	if !strings.Contains(cmd, "aidev") {
		t.Errorf("command = %q, want it to contain \"aidev\"", cmd)
	}
}

// An error path (UNSUPPORTED_OUTPUT) writes exactly one audit line: outcome
// "error" with a code.
func TestRunAuditsErrorOnce(t *testing.T) {
	home := writeHome(t)
	var buf bytes.Buffer
	if code := Run(&buf, t.TempDir(), "yaml", false); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	ls := auditLines(t, home)
	if len(ls) != 1 {
		t.Fatalf("want exactly 1 audit line, got %d: %+v", len(ls), ls)
	}
	if ls[0]["outcome"] != "error" {
		t.Errorf("outcome = %v, want error", ls[0]["outcome"])
	}
	if code, _ := ls[0]["code"].(string); code == "" {
		t.Errorf("error line must carry a code: %+v", ls[0])
	}
}

func TestRunJSONEnvelope(t *testing.T) {
	writeHome(t)
	t.Setenv("AIDEV_SCENE", "companyA")
	var buf bytes.Buffer
	code := Run(&buf, t.TempDir(), "json", false)
	if code != 0 {
		t.Fatalf("code = %d, out = %s", code, buf.String())
	}
	var env struct {
		Data Inventory `json:"data"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, buf.String())
	}
	if env.Data.Workspace.Source != "AIDEV_SCENE" {
		t.Errorf("source = %s, want AIDEV_SCENE", env.Data.Workspace.Source)
	}
	if _, ok := env.Data.Targets["a-uat"]; !ok {
		t.Error("a-uat missing from targets")
	}
}

func TestRunBadOutput(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	var buf bytes.Buffer
	code := Run(&buf, t.TempDir(), "yaml", false)
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(buf.String(), "UNSUPPORTED_OUTPUT") {
		t.Errorf("want UNSUPPORTED_OUTPUT, got %s", buf.String())
	}
}
