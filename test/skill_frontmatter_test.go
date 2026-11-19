// Package test holds repo-hygiene checks that do not belong to a single CLI.
package test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// maxDescriptionLen is what agent skill loaders accept before they truncate or
// drop the skill.
const maxDescriptionLen = 1024

// skillFrontmatter is the Agents-standard frontmatter every skills/<name>/SKILL.md
// must carry. Add new keys here — unknown keys are rejected so a typo like
// `descripton:` cannot silently produce a keyless skill.
type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// TestSkillFrontmatter validates skills/<name>/SKILL.md before the installers
// copy it verbatim into ~/.agents|~/.claude|~/.codex and goreleaser ships it in
// the release archive — both are unfiltered, so a malformed frontmatter block
// reaches users as a skill the agent's loader silently drops.
//
// Written for a real incident: an UNQUOTED `description` containing
// `expect: list` (colon + space) parses as a nested mapping, so the entire
// frontmatter failed to load and the tcli skill vanished from every harness.
// If a description contains ": ", it must be quoted.
func TestSkillFrontmatter(t *testing.T) {
	skillsDir := filepath.Join("..", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("read %s: %v", skillsDir, err)
	}

	skills := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skills++
		dir := e.Name()
		t.Run(dir, func(t *testing.T) {
			path := filepath.Join(skillsDir, dir, "SKILL.md")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}

			body, err := splitFrontmatter(raw)
			if err != nil {
				t.Fatalf("%v", err)
			}

			var fm skillFrontmatter
			dec := yaml.NewDecoder(bytes.NewReader(body))
			dec.KnownFields(true)
			if err := dec.Decode(&fm); err != nil {
				t.Fatalf("frontmatter is not valid YAML: %v\n"+
					"hint: a description containing \": \" (or a leading #/-/[/{ etc.) must be quoted", err)
			}

			if fm.Name != dir {
				t.Errorf("name = %q, want %q (must match the skill directory)", fm.Name, dir)
			}
			if strings.TrimSpace(fm.Description) == "" {
				t.Error("description is empty (the loader needs it to route the skill)")
			}
			if strings.TrimSpace(fm.Description) != fm.Description {
				t.Errorf("description has leading/trailing whitespace: %q", fm.Description)
			}
			if strings.ContainsAny(fm.Description, "\n\r") {
				t.Errorf("description must be a single line, got %q", fm.Description)
			}
			if len(fm.Description) > maxDescriptionLen {
				t.Errorf("description is %d bytes, want <= %d", len(fm.Description), maxDescriptionLen)
			}
		})
	}
	if skills == 0 {
		t.Fatalf("no skill directories under %s (wrong working dir?)", skillsDir)
	}
}

// splitFrontmatter returns the YAML between the opening `---` and the next `---`.
// Line endings are normalized first: a Windows checkout with core.autocrlf
// turns SKILL.md into CRLF (see docs/CROSS-PLATFORM.md), and the CI matrix runs
// this test on windows-latest.
func splitFrontmatter(raw []byte) ([]byte, error) {
	text := strings.TrimPrefix(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\ufeff")
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSuffix(lines[0], "\r") != "---" {
		return nil, errors.New("SKILL.md must start with a `---` frontmatter block")
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSuffix(lines[i], "\r") != "---" {
			continue
		}
		return []byte(strings.Join(lines[1:i], "\n")), nil
	}
	return nil, errors.New("unterminated frontmatter block (no closing `---`)")
}
