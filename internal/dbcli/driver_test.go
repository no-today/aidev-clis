package dbcli

import (
	"context"
	"testing"
)

type fakeDriver struct{ name string }

func (f fakeDriver) Name() string                             { return f.name }
func (f fakeDriver) Run(context.Context, Input, Output) error { return nil }

func TestRegistry_Get(t *testing.T) {
	r := NewRegistry([]Driver{fakeDriver{"mysql"}, fakeDriver{"redis"}})
	if _, ok := r.Get("mysql"); !ok {
		t.Fatal("mysql should be registered")
	}
	if _, ok := r.Get("nope"); ok {
		t.Fatal("unknown driver must not resolve")
	}
}
