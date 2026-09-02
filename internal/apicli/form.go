package apicli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// FormPart is one parsed -F argument. A part is a plain field when File is
// empty (its value lives in Value), otherwise a file field. Bytes is filled in
// by EncodeForm from the stat it already performs, so the audit record can
// report sizes without a second stat.
type FormPart struct {
	Name        string
	Value       string
	File        string
	Filename    string
	ContentType string
	Bytes       int64
}

// ParseFormArgs splits raw -F arguments into parts. Pure: it never touches disk,
// so a bad argument is rejected before any file is opened.
//
// A value beginning with "@" names a file. The ";type=" and ";filename="
// modifiers are parsed ONLY on that form — for a plain field, everything after
// the first "=" is the literal value, so a value containing ";" is never
// silently truncated. curl parses ";type=" on plain fields too; we trade that
// rarity for the guarantee.
func ParseFormArgs(args []string) ([]FormPart, error) {
	var parts []FormPart
	for _, a := range args {
		name, rest, ok := strings.Cut(a, "=")
		if !ok || name == "" {
			return nil, errs.Config("FORM_ARG_INVALID",
				fmt.Sprintf("-F must be name=value or name=@file: %q", a))
		}
		spec, isFile := strings.CutPrefix(rest, "@")
		if !isFile {
			parts = append(parts, FormPart{Name: name, Value: rest})
			continue
		}
		fields := strings.Split(spec, ";")
		p := FormPart{Name: name, File: fields[0]}
		if p.File == "" {
			return nil, errs.Config("FORM_ARG_INVALID",
				fmt.Sprintf("-F %q has an empty file path", a))
		}
		for _, mod := range fields[1:] {
			k, v, ok := strings.Cut(strings.TrimSpace(mod), "=")
			if !ok {
				return nil, errs.Config("FORM_ARG_INVALID",
					fmt.Sprintf("-F %q: modifier %q must be key=value", a, mod))
			}
			switch k {
			case "type":
				p.ContentType = v
			case "filename":
				p.Filename = v
			default:
				return nil, errs.Config("FORM_ARG_INVALID",
					fmt.Sprintf("-F %q: unknown modifier %q (supported: type, filename)", a, k))
			}
		}
		if p.Filename == "" {
			p.Filename = filepath.Base(p.File)
		}
		parts = append(parts, p)
	}
	return parts, nil
}
