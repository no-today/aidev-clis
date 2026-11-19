package main

import "testing"

func TestRootHasThreeCommands(t *testing.T) {
	root := newRoot()
	got := map[string]bool{}
	for _, c := range root.Commands() {
		got[c.Name()] = true
	}
	for _, want := range []string{"run", "validate", "explain"} {
		if !got[want] {
			t.Fatalf("missing subcommand %q", want)
		}
	}
	if got["summary"] {
		t.Fatal("summary must NOT exist (dropped)")
	}
}
