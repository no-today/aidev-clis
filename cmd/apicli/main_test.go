package main

import (
	"bytes"
	"testing"
)

func TestRootHasSubcommands(t *testing.T) {
	root := newRoot()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("help: %v", err)
	}
	for _, sub := range []string{"call", "login", "whoami", "logout"} {
		if !bytes.Contains(buf.Bytes(), []byte(sub)) {
			t.Errorf("root help missing %q", sub)
		}
	}
}

func TestConfigDirFlagSetsHome(t *testing.T) {
	dir := t.TempDir()
	// no apicli.yaml in dir -> resolve must fail with CONFIG_MISSING,
	// proving --config-dir pointed config loading at dir.
	out := runCLI(t, "--config-dir", dir, "call", "shop", "/x")
	if !bytes.Contains(out, []byte("CONFIG_MISSING")) {
		t.Fatalf("expected CONFIG_MISSING from empty --config-dir, got: %s", out)
	}
}
