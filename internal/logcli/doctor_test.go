package logcli

import (
	"testing"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

type capDoctorOut struct{ batched []any }

func (c *capDoctorOut) Batch(r []any, _ ...string) error { c.batched = r; return nil }
func (c *capDoctorOut) Stream() Streamer                 { panic("doctor must not stream") }

func TestDoctor_OK(t *testing.T) {
	out := &capDoctorOut{}
	if err := Doctor(out, "cfg detail", "connected", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if len(out.batched) != 2 {
		t.Fatalf("want 2 checks, got %d", len(out.batched))
	}
	c0, c1 := out.batched[0].(Check), out.batched[1].(Check)
	if c0.Name != "config" || !c0.OK || c1.Name != "connect" || !c1.OK || c1.Detail != "connected" {
		t.Fatalf("checks: %+v", out.batched)
	}
}

func TestDoctor_FailEmitsChecksAndReturnsExit(t *testing.T) {
	out := &capDoctorOut{}
	err := Doctor(out, "cfg", "ok", func() error { return errs.Remote("PROBE_DOWN", "unreachable") })
	if err == nil {
		t.Fatal("want error to carry the non-zero exit")
	}
	if len(out.batched) != 2 || out.batched[1].(Check).OK {
		t.Fatalf("connect should be ok=false but emitted: %+v", out.batched)
	}
}
