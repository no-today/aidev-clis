package main

import "testing"

func TestWireCompletions_SubcommandsCompleteCaseFiles(t *testing.T) {
	root := newRoot()
	for _, name := range []string{"run", "validate", "explain"} {
		var found bool
		for _, c := range root.Commands() {
			if c.Name() == name {
				found = true
				if c.ValidArgsFunction == nil {
					t.Fatalf("%s: ValidArgsFunction not wired", name)
				}
			}
		}
		if !found {
			t.Fatalf("subcommand %q missing", name)
		}
	}
}
