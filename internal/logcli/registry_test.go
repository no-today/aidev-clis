package logcli

import (
	"context"
	"testing"
)

type registryAdapter struct{}

func (registryAdapter) Name() string                             { return "sls" }
func (registryAdapter) Run(context.Context, Input, Output) error { return nil }

func TestRegistry_RegistersOnlyConfiguredName(t *testing.T) {
	r := NewRegistry([]Adapter{registryAdapter{}})
	if _, ok := r.Get("sls"); !ok {
		t.Fatal("registered name must resolve")
	}
	if _, ok := r.Get("aliyun-sls"); ok {
		t.Fatal("old adapter name must not resolve")
	}
	if r.Canonical("sls") != "sls" {
		t.Fatalf("Canonical(sls) = %q, want sls", r.Canonical("sls"))
	}
	if r.Canonical("nope") != "" {
		t.Fatal("unknown name must canonicalize to empty")
	}
}
