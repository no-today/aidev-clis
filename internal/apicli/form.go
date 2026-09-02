package apicli

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"os"
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

// EncodeForm reads the referenced files and encodes parts into a complete
// multipart body, returned with the matching Content-Type (boundary included).
//
// Every file is stat-ed and the sizes summed BEFORE any content is read, so an
// over-cap request fails without allocating — but that guarantee only holds
// for a REGULAR file. os.Stat reports Size() == 0 for a FIFO, character
// device, or socket, so the cap check would pass trivially and the later
// io.Copy would then read an unbounded (or blocking) stream into memory. Only
// regular files are accepted, which is what makes "stat and sum before
// reading any bytes" true in general, not just for the common case. The stat
// sizes are recorded back into parts so the audit record can report them
// without a second stat.
//
// The body is a plain []byte on purpose: Call replays it verbatim on the
// auto-relogin retry, with no disk re-read and no boundary change.
func EncodeForm(parts []FormPart, maxBytes int64) ([]byte, string, error) {
	var total int64
	for i := range parts {
		if parts[i].File == "" {
			continue
		}
		st, err := os.Stat(parts[i].File)
		if err != nil {
			return nil, "", errs.General("FORM_FILE_UNREADABLE", err.Error())
		}
		if !st.Mode().IsRegular() {
			return nil, "", errs.General("FORM_FILE_UNREADABLE",
				parts[i].File+" is not a regular file")
		}
		parts[i].Bytes = st.Size()
		total += st.Size()
	}
	if total > maxBytes {
		return nil, "", errs.General("FORM_TOO_LARGE",
			fmt.Sprintf("upload totals %d bytes, over the %d-byte cap; raise it with --max-upload",
				total, maxBytes))
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, p := range parts {
		if p.File == "" {
			if err := w.WriteField(p.Name, p.Value); err != nil {
				return nil, "", errs.General("FORM_ENCODE_FAILED", err.Error())
			}
			continue
		}
		// CreatePart, not CreateFormFile: the latter hardcodes
		// application/octet-stream and would drop the inferred/explicit type.
		ct := p.ContentType
		if ct == "" {
			ct = mime.TypeByExtension(filepath.Ext(p.Filename))
		}
		if ct == "" {
			ct = "application/octet-stream"
		}
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`,
			escapeQuotes(p.Name), escapeQuotes(p.Filename)))
		h.Set("Content-Type", ct)
		fw, err := w.CreatePart(h)
		if err != nil {
			return nil, "", errs.General("FORM_ENCODE_FAILED", err.Error())
		}
		f, err := os.Open(p.File)
		if err != nil {
			return nil, "", errs.General("FORM_FILE_UNREADABLE", err.Error())
		}
		_, copyErr := io.Copy(fw, f)
		_ = f.Close()
		if copyErr != nil {
			return nil, "", errs.General("FORM_FILE_UNREADABLE", copyErr.Error())
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", errs.General("FORM_ENCODE_FAILED", err.Error())
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

// quoteEscaper mirrors mime/multipart's own Content-Disposition quoting.
var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

func escapeQuotes(s string) string { return quoteEscaper.Replace(s) }

// formArgString re-renders a parsed part as the -F argument a user would type.
// ToCurl uses it so the printed command is a runnable equivalent.
func formArgString(p FormPart) string {
	if p.File == "" {
		return p.Name + "=" + p.Value
	}
	s := p.Name + "=@" + p.File
	if p.Filename != "" && p.Filename != filepath.Base(p.File) {
		s += ";filename=" + p.Filename
	}
	if p.ContentType != "" {
		s += ";type=" + p.ContentType
	}
	return s
}
