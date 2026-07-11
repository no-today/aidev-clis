package tcli

import (
	"strings"
	"testing"
)

func TestCheckVars_UseBeforeCapture(t *testing.T) {
	y := `name: t
apps:
  orders: {}
steps:
  - name: a
    api:
      request: "GET /{{order_id}}"
  - name: b
    api:
      request: "GET /y"
      capture:
        order_id: body.id
`
	c, _ := parseCaseBytes([]byte(y), "t.yaml")
	d := c.Validate()
	if !containsSub(d, "order_id") {
		t.Fatalf("expected use-before-capture diagnostic, got %v", d)
	}
}

func TestCheckVars_BuiltinsAndCaptureOK(t *testing.T) {
	y := `name: t
apps:
  orders: {}
vars:
  customer: C1
steps:
  - name: a
    api:
      request: "POST /o"
      capture:
        oid: body.id
  - name: b
    api:
      request: "GET /o/{{oid}}"
`
	c, _ := parseCaseBytes([]byte(y), "t.yaml")
	if d := c.Validate(); len(d) != 0 {
		t.Fatalf("expected no diagnostics, got %v", d)
	}
}

func TestCheckVars_BuiltinRunIDAvailable(t *testing.T) {
	y := `name: t
apps:
  orders: {}
steps:
  - name: a
    api:
      request: "POST /o/{{run_id}}"
`
	c, _ := parseCaseBytes([]byte(y), "t.yaml")
	if d := c.Validate(); len(d) != 0 {
		t.Fatalf("run_id is a builtin and should be available, got %v", d)
	}
}

func TestCheckVars_TraceIDFromPriorAPIStep(t *testing.T) {
	// trace_id is auto-seeded after any api step
	y := `name: t
apps:
  orders: {}
logs:
  sls: orders_sls
steps:
  - name: call
    api:
      request: "GET /x"
  - name: check_log
    log:
      trace: "{{trace_id}}"
`
	c, _ := parseCaseBytes([]byte(y), "t.yaml")
	if d := c.Validate(); len(d) != 0 {
		t.Fatalf("trace_id should be available after api step, got %v", d)
	}
}

func containsSub(ss []string, sub string) bool {
	for _, s := range ss {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
