package main

import (
	"reflect"
	"testing"

	"github.com/spf13/cobra"
)

func TestCompleteConfig_FirstTokenOffersVerbs(t *testing.T) {
	got, dir := completeConfigArgs(nil, "")
	if !reflect.DeepEqual(got, []string{"backup", "restore"}) {
		t.Fatalf("first token: got %v", got)
	}
	if dir != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("directive: got %v", dir)
	}
}

func TestCompleteConfig_FlagNamesScopedToVerb(t *testing.T) {
	got, _ := completeConfigArgs([]string{"backup"}, "--")
	if !reflect.DeepEqual(got, []string{"--dest-dir", "--output", "--pretty"}) {
		t.Fatalf("backup flags: got %v", got)
	}
	got, _ = completeConfigArgs([]string{"restore"}, "--")
	if !reflect.DeepEqual(got, []string{"--backup-dir", "--no-backup", "--output", "--pretty"}) {
		t.Fatalf("restore flags: got %v", got)
	}
}

func TestCompleteConfig_OutputValues(t *testing.T) {
	got, dir := completeConfigArgs([]string{"backup", "--output"}, "")
	if !reflect.DeepEqual(got, []string{"json", "raw"}) {
		t.Fatalf("--output values: got %v", got)
	}
	if dir != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("--output directive: got %v", dir)
	}
}

func TestCompleteConfig_DirFlagsGetDirCompletion(t *testing.T) {
	for _, flag := range []string{"--dest-dir", "--backup-dir"} {
		if _, dir := completeConfigArgs([]string{"backup", flag}, ""); dir != cobra.ShellCompDirectiveFilterDirs {
			t.Fatalf("%s directive: got %v", flag, dir)
		}
	}
}

func TestCompleteConfig_RestoreArchiveGetsFileCompletion(t *testing.T) {
	// restore with no archive yet → file completion.
	if got, dir := completeConfigArgs([]string{"restore"}, ""); got != nil || dir != cobra.ShellCompDirectiveDefault {
		t.Fatalf("restore archive: got %v / %v", got, dir)
	}
	// archive already given → no more positionals.
	if got, dir := completeConfigArgs([]string{"restore", "snap.tar.gz"}, ""); got != nil || dir != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("restore after archive: got %v / %v", got, dir)
	}
	// backup takes no positional archive.
	if got, _ := completeConfigArgs([]string{"backup"}, ""); got != nil {
		t.Fatalf("backup positional: expected nil, got %v", got)
	}
}

func TestCompleteConfig_FlagsBeforeVerbSkippedInScan(t *testing.T) {
	// A value-flag and its value before the verb must not be mistaken for the verb.
	got, _ := completeConfigArgs([]string{"restore", "--backup-dir", "/tmp"}, "--")
	if !reflect.DeepEqual(got, []string{"--backup-dir", "--no-backup", "--output", "--pretty"}) {
		t.Fatalf("restore flags after --backup-dir value: got %v", got)
	}
}
