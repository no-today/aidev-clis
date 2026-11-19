package aidev

import (
	"fmt"
	"io"
	"strings"

	"github.com/no-today/aidev-clis/internal/aidev/configarchive"
)

// RenderRaw writes the human/un-enveloped summary of the inventory (--output raw).
func RenderRaw(w io.Writer, inv Inventory) {
	scene := "(none)"
	if inv.Workspace.Scene != nil {
		scene = *inv.Workspace.Scene
	}
	fmt.Fprintf(w, "scene: %s (source %s)\n", scene, inv.Workspace.Source)
	fmt.Fprintf(w, "tools: %s\n", strings.Join(inv.Tools, ", "))
	if len(inv.Targets) > 0 {
		fmt.Fprintln(w, "targets:")
		for _, name := range sortedCapKeys(inv.Targets) {
			fmt.Fprintf(w, "  %s\t%s\n", name, strings.Join(inv.Targets[name].CLIs, ", "))
		}
	}
	if len(inv.Apps) > 0 {
		fmt.Fprintf(w, "apps: %s\n", strings.Join(inv.Apps, ", "))
	}
	for _, n := range inv.Notes {
		fmt.Fprintf(w, "note: %s\n", n)
	}
}

// RenderConfigRaw writes a human summary of a config backup/restore result.
func RenderConfigRaw(w io.Writer, data any) {
	switch r := data.(type) {
	case *configarchive.ArchiveResult:
		fmt.Fprintf(w, "backed up %d file(s) -> %s\n", r.Count, r.Path)
		for _, e := range r.Entries {
			fmt.Fprintf(w, "  %s\t%d\t%s\n", e.Path, e.Size, e.Mode)
		}
	case *configarchive.RestoreResult:
		fmt.Fprintf(w, "restored %d file(s) from %s\n", r.Count, r.Archive)
		if r.BackupPath != "" {
			fmt.Fprintf(w, "safety backup: %s\n", r.BackupPath)
		}
		for _, e := range r.Entries {
			fmt.Fprintf(w, "  %s\t%d\t%s\n", e.Path, e.Size, e.Mode)
		}
	default:
		fmt.Fprintf(w, "unrenderable result: %T\n", data)
	}
}
