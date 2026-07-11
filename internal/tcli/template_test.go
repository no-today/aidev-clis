package tcli

import (
	"strings"
	"testing"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

func TestRender_SubstitutesAndPreservesText(t *testing.T) {
	got, err := Render("id={{order_id}} u={{user}}", map[string]string{"order_id": "ORD-1", "user": "bob"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "id=ORD-1 u=bob" {
		t.Fatalf("got %q", got)
	}
}

func TestRender_MissingVarErrors(t *testing.T) {
	_, err := Render("x={{nope}}", map[string]string{})
	if err == nil {
		t.Fatal("expected error")
	}
	if e := errs.From(err); e.Code != "TEMPLATE_VAR_MISSING" || !strings.Contains(e.Message, "nope") {
		t.Fatalf("got code=%q msg=%q", e.Code, e.Message)
	}
}

func TestRender_NoPlaceholders(t *testing.T) {
	got, err := Render("plain text", nil)
	if err != nil || got != "plain text" {
		t.Fatalf("got %q err %v", got, err)
	}
}

// RenderMap 渲染 map 的每个 value(用于 headers/inline actor vars)。
func TestRenderMap(t *testing.T) {
	out, err := RenderMap(map[string]string{"h": "{{tok}}"}, map[string]string{"tok": "abc"})
	if err != nil || out["h"] != "abc" {
		t.Fatalf("got %v err %v", out, err)
	}
}
