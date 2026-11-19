package diag

import (
	"context"
	"testing"
)

func TestLogf_RecordsAtOrBelowLevel(t *testing.T) {
	d := New(1) // -v
	d.Logf(1, "resolved env=%s", "prod")
	d.Logf(2, "argv=%v", []string{"psql"}) // -vv detail: dropped at level 1
	got := d.Lines()
	if len(got) != 1 {
		t.Fatalf("want 1 line at level 1, got %d: %v", len(got), got)
	}
	if got[0] != "resolved env=prod" {
		t.Fatalf("bad line: %q", got[0])
	}
}

func TestLogf_LevelTwoRecordsBoth(t *testing.T) {
	d := New(2) // -vv
	d.Logf(1, "a")
	d.Logf(2, "b")
	if got := d.Lines(); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("want [a b] in order, got %v", got)
	}
}

func TestLogf_CapsUnboundedGrowth(t *testing.T) {
	d := New(1)
	for i := 0; i < maxLines+500; i++ {
		d.Logf(1, "line %d", i)
	}
	got := d.Lines()
	if len(got) != maxLines+1 { // maxLines kept + one truncation marker
		t.Fatalf("want %d lines (cap + marker), got %d", maxLines+1, len(got))
	}
	if want := "… diagnostics truncated (10000-line cap reached)"; got[len(got)-1] != want {
		t.Fatalf("last line = %q, want truncation marker", got[len(got)-1])
	}
}

func TestLevelZero_RecordsNothing(t *testing.T) {
	d := New(0) // no -v: silent
	d.Logf(1, "should not appear")
	if got := d.Lines(); len(got) != 0 {
		t.Fatalf("level 0 must record nothing, got %v", got)
	}
}

func TestNilDiag_IsNoOp(t *testing.T) {
	var d *Diag // absent collector
	d.Logf(1, "no panic")
	if got := d.Lines(); got != nil {
		t.Fatalf("nil Diag Lines must be nil, got %v", got)
	}
}

func TestFrom_AbsentReturnsNil(t *testing.T) {
	if d := From(context.Background()); d != nil {
		t.Fatalf("From on bare ctx must be nil, got %v", d)
	}
}

func TestFrom_NilContext_NoPanic(t *testing.T) {
	//nolint:staticcheck // deliberately passing a nil context: From must not panic
	if d := From(nil); d != nil {
		t.Fatalf("From(nil) must be nil, got %v", d)
	}
}

func TestWithFrom_Roundtrip(t *testing.T) {
	d := New(2)
	ctx := With(context.Background(), d)
	if From(ctx) != d {
		t.Fatal("From must return the Diag put in by With")
	}
}
