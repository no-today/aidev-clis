# apicli `-F/--form` Multipart Upload Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give `apicli call` a curl-shaped `-F/--form` flag that reads file bytes straight from disk and computes the multipart boundary itself, so binary uploads never pass through a shell variable.

**Architecture:** Two pure functions in a new `internal/apicli/form.go` — `ParseFormArgs` (no disk I/O) and `EncodeForm` (stat, read, encode). The command layer encodes **once** into `req.Body` and hands `DoRequest` a precomputed `Content-Type`; because the body is a plain `[]byte`, the auto-relogin retry in `Call` replays it byte-for-byte with no disk re-read.

**Tech Stack:** Go, stdlib `mime/multipart` + `mime` + `net/textproto`, cobra flags, `internal/core/errs` for typed errors, `internal/core/audit` for the audit trail.

## Global Constraints

Copied from the spec (`docs/superpowers/specs/2026-09-02-apicli-form-upload-design.md`). Every task's requirements implicitly include these.

- **Work in the worktree** `/Users/whoog/Dev/aidev-clis-form-upload` on branch `form-upload`. Never edit the live tree at `/Users/whoog/Dev/aidev-clis` — concurrent sessions share it and commits interleave.
- **Adapter isolation:** no cross-CLI Go imports. This work touches only `internal/apicli/`, `cmd/apicli/`, `docs/`, and `skills/aidev-apicli/`.
- **`-h` is the truth source for flags.** Flag help text is authoritative; docs describe the durable model, not a flag enumeration.
- **AI-first output:** the JSON envelope stays the default. This feature does not change the response envelope at all.
- **Cross-platform:** CI runs Linux/macOS/**Windows**. Use `filepath.Base` / `filepath.Ext` for paths, generate every test fixture into `t.TempDir()`, never commit a binary fixture.
- **Body must be replayable.** `Call` (`internal/apicli/call.go:71`) calls `DoRequest` a second time after a re-login. Anything consumed by the first send breaks auto-relogin. This is why the body is a buffered `[]byte` and why stdin is a non-goal.
- **Never audit field values or file content.** Only names, paths, sizes.
- **Default upload cap:** `512MB`, overridable with `--max-upload`.
- **Run `make check` before the final commit** — gofmt + vet + build + isolation guards + tests, and it crossbuilds all three GOOS.

---

### Task 1: `ParseFormArgs` — parse `-F` arguments, no disk I/O

**Files:**
- Create: `internal/apicli/form.go`
- Test: `internal/apicli/form_test.go`

**Interfaces:**
- Consumes: `internal/core/errs` (`errs.Config`).
- Produces:
  - `type FormPart struct { Name, Value, File, Filename, ContentType string; Bytes int64 }`
  - `func ParseFormArgs(args []string) ([]FormPart, error)`

  A part is a **plain field** when `File == ""` (value in `Value`), otherwise a **file field**. `Bytes` is left zero here; Task 2 fills it.

- [ ] **Step 1: Write the failing test**

Create `internal/apicli/form_test.go`:

```go
package apicli

import "testing"

func TestParseFormArgs(t *testing.T) {
	got, err := ParseFormArgs([]string{
		"entranceExitId=11085",
		"note=a;b=c",
		"images=@/tmp/a.png",
		"images=@/tmp/b.png;type=image/png",
		"doc=@/tmp/c.bin;filename=report.pdf",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("want 5 parts, got %d: %+v", len(got), got)
	}
	// Plain field: everything after the first "=" is literal, semicolons included.
	if got[0].Name != "entranceExitId" || got[0].Value != "11085" || got[0].File != "" {
		t.Errorf("plain field wrong: %+v", got[0])
	}
	if got[1].Value != "a;b=c" {
		t.Errorf("plain value must keep semicolons verbatim, got %q", got[1].Value)
	}
	// File field: filename defaults to the base name, no type inferred yet.
	if got[2].File != "/tmp/a.png" || got[2].Filename != "a.png" || got[2].ContentType != "" {
		t.Errorf("file field wrong: %+v", got[2])
	}
	// Modifiers.
	if got[3].ContentType != "image/png" || got[3].Filename != "b.png" {
		t.Errorf("type modifier wrong: %+v", got[3])
	}
	if got[4].Filename != "report.pdf" || got[4].File != "/tmp/c.bin" {
		t.Errorf("filename modifier wrong: %+v", got[4])
	}
	// Repeated name is preserved in order — this is the Spring List<MultipartFile> case.
	if got[2].Name != "images" || got[3].Name != "images" {
		t.Errorf("repeated name not preserved: %+v %+v", got[2], got[3])
	}
}

func TestParseFormArgsErrors(t *testing.T) {
	for _, arg := range []string{
		"noequals",             // no "="
		"=value",               // empty name
		"f=@",                  // empty file path
		"f=@/tmp/x;bogus=1",    // unknown modifier
		"f=@/tmp/x;novalue",    // modifier without "="
	} {
		if _, err := ParseFormArgs([]string{arg}); err == nil {
			t.Errorf("expected error for -F %q, got nil", arg)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/whoog/Dev/aidev-clis-form-upload && go test ./internal/apicli/ -run TestParseFormArgs -v`

Expected: FAIL — `undefined: ParseFormArgs`, `undefined: FormPart`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/apicli/form.go`:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/whoog/Dev/aidev-clis-form-upload && go test ./internal/apicli/ -run TestParseFormArgs -v`

Expected: PASS — both `TestParseFormArgs` and `TestParseFormArgsErrors`.

- [ ] **Step 5: Commit**

```bash
cd /Users/whoog/Dev/aidev-clis-form-upload
git add internal/apicli/form.go internal/apicli/form_test.go
git commit -m "Add ParseFormArgs for apicli -F arguments"
```

---

### Task 2: `EncodeForm` — stat, cap, read, encode

**Files:**
- Modify: `internal/apicli/form.go` (append)
- Test: `internal/apicli/form_test.go` (append)

**Interfaces:**
- Consumes: `FormPart`, `ParseFormArgs` from Task 1.
- Produces:
  - `func EncodeForm(parts []FormPart, maxBytes int64) (body []byte, contentType string, err error)` — mutates `parts[i].Bytes` in place for file parts.
  - `func formArgString(p FormPart) string` — re-renders a part as the `-F` argument a user would type (used by `ToCurl` in Task 4; unexported, same package).

- [ ] **Step 1: Write the failing test**

Append to `internal/apicli/form_test.go`:

```go
import (
	"bytes"
	"crypto/sha256"
	"io"
	"mime"
	"mime/multipart"
	"os"
	"path/filepath"
	"testing"
)

// writeFixture writes content into t.TempDir() and returns the path. Fixtures
// are generated, never committed — see docs/CROSS-PLATFORM.md.
func writeFixture(t *testing.T, name string, content []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, content, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return p
}

// decodeParts re-reads an encoded body with the stdlib reader so assertions are
// made against what a real server would see, not against our own encoder.
func decodeParts(t *testing.T, body []byte, contentType string) []*multipart.Part {
	t.Helper()
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		t.Fatalf("bad content type %q: %v", contentType, err)
	}
	r := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	var out []*multipart.Part
	for {
		p, err := r.NextPart()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatalf("next part: %v", err)
		}
		out = append(out, p)
	}
}

func TestEncodeFormRoundTrip(t *testing.T) {
	a := writeFixture(t, "a.png", []byte("AAA"))
	b := writeFixture(t, "b.dat", []byte("BBBB"))
	parts, err := ParseFormArgs([]string{
		"entranceExitId=11085",
		"images=@" + a,
		"images=@" + b + ";type=image/jpeg;filename=renamed.jpg",
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	body, ct, err := EncodeForm(parts, 1<<20)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got := decodeParts(t, body, ct)
	if len(got) != 3 {
		t.Fatalf("want 3 parts, got %d", len(got))
	}
	// Order is preserved, and both file parts keep the same field name —
	// this is what binds to a Spring List<MultipartFile>.
	if got[0].FormName() != "entranceExitId" || got[0].FileName() != "" {
		t.Errorf("part 0 should be a plain field: name=%q file=%q", got[0].FormName(), got[0].FileName())
	}
	v, _ := io.ReadAll(got[0])
	if string(v) != "11085" {
		t.Errorf("plain value = %q, want 11085", v)
	}
	if got[1].FormName() != "images" || got[1].FileName() != "a.png" {
		t.Errorf("part 1 wrong: name=%q file=%q", got[1].FormName(), got[1].FileName())
	}
	// .png is inferred from the extension.
	if ct1 := got[1].Header.Get("Content-Type"); ct1 != "image/png" {
		t.Errorf("part 1 content-type = %q, want image/png", ct1)
	}
	// Explicit ;type= and ;filename= win over inference.
	if got[2].FileName() != "renamed.jpg" {
		t.Errorf("part 2 filename = %q, want renamed.jpg", got[2].FileName())
	}
	if ct2 := got[2].Header.Get("Content-Type"); ct2 != "image/jpeg" {
		t.Errorf("part 2 content-type = %q, want image/jpeg", ct2)
	}
	// Sizes are recorded back into parts for the audit record.
	if parts[1].Bytes != 3 || parts[2].Bytes != 4 {
		t.Errorf("sizes not recorded: %d %d", parts[1].Bytes, parts[2].Bytes)
	}
	if parts[0].Bytes != 0 {
		t.Errorf("plain field must not get a size, got %d", parts[0].Bytes)
	}
}

// TestEncodeFormIsBinarySafe pins the two failure modes that motivated this
// flag: command substitution strips trailing newlines, and shell variables
// truncate at NUL. Bytes must survive verbatim.
func TestEncodeFormIsBinarySafe(t *testing.T) {
	raw := []byte{0x89, 'P', 'N', 'G', 0x00, 0x00, '\r', '\n', 0xFF, '\n', '\n'}
	f := writeFixture(t, "bin.png", raw)
	parts, err := ParseFormArgs([]string{"images=@" + f})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	body, ct, err := EncodeForm(parts, 1<<20)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got := decodeParts(t, body, ct)
	if len(got) != 1 {
		t.Fatalf("want 1 part, got %d", len(got))
	}
	back, err := io.ReadAll(got[0])
	if err != nil {
		t.Fatalf("read part: %v", err)
	}
	if sha256.Sum256(back) != sha256.Sum256(raw) {
		t.Fatalf("bytes altered in transit:\n got %x\nwant %x", back, raw)
	}
}

func TestEncodeFormErrors(t *testing.T) {
	big := writeFixture(t, "big.bin", make([]byte, 100))
	parts, _ := ParseFormArgs([]string{"f=@" + big})
	if _, _, err := EncodeForm(parts, 50); err == nil {
		t.Error("expected FORM_TOO_LARGE when the total exceeds the cap")
	}

	missing, _ := ParseFormArgs([]string{"f=@" + filepath.Join(t.TempDir(), "nope.bin")})
	if _, _, err := EncodeForm(missing, 1<<20); err == nil {
		t.Error("expected FORM_FILE_UNREADABLE for a missing file")
	}

	dir, _ := ParseFormArgs([]string{"f=@" + t.TempDir()})
	if _, _, err := EncodeForm(dir, 1<<20); err == nil {
		t.Error("expected FORM_FILE_UNREADABLE for a directory")
	}
}

func TestFormArgString(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"entranceExitId=11085", "entranceExitId=11085"},
		{"images=@/tmp/a.png", "images=@/tmp/a.png"},
		{"images=@/tmp/a.png;type=image/png", "images=@/tmp/a.png;type=image/png"},
		{"d=@/tmp/c.bin;filename=r.pdf", "d=@/tmp/c.bin;filename=r.pdf"},
	} {
		parts, err := ParseFormArgs([]string{tc.in})
		if err != nil {
			t.Fatalf("parse %q: %v", tc.in, err)
		}
		if got := formArgString(parts[0]); got != tc.want {
			t.Errorf("formArgString(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/whoog/Dev/aidev-clis-form-upload && go test ./internal/apicli/ -run 'TestEncodeForm|TestFormArgString' -v`

Expected: FAIL — `undefined: EncodeForm`, `undefined: formArgString`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/apicli/form.go`, and extend its import block to:

```go
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
```

```go
// EncodeForm reads the referenced files and encodes parts into a complete
// multipart body, returned with the matching Content-Type (boundary included).
//
// Every file is stat-ed and the sizes summed BEFORE any content is read, so an
// over-cap request fails without allocating. The stat sizes are recorded back
// into parts so the audit record can report them without a second stat.
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
		if st.IsDir() {
			return nil, "", errs.General("FORM_FILE_UNREADABLE",
				parts[i].File+" is a directory, not a file")
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
```

Note on `mime.TypeByExtension`: it returns values like `image/png` on all
platforms for common extensions, but may append `; charset=utf-8` for text
types. That is correct MIME and servers accept it — do not strip it.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/whoog/Dev/aidev-clis-form-upload && go test ./internal/apicli/ -v -run 'TestParseFormArgs|TestEncodeForm|TestFormArgString'`

Expected: PASS — all six tests from Tasks 1 and 2.

- [ ] **Step 5: Commit**

```bash
cd /Users/whoog/Dev/aidev-clis-form-upload
git add internal/apicli/form.go internal/apicli/form_test.go
git commit -m "Add EncodeForm: buffered, replayable multipart encoding"
```

---

### Task 3: `parseUploadSize` — the `--max-upload` flag value

**Files:**
- Modify: `cmd/apicli/commands.go` (append a helper near the bottom, beside `headerMap`)
- Test: `cmd/apicli/commands_test.go` (append)

**Interfaces:**
- Consumes: `internal/core/errs` (`errs.Config`).
- Produces: `func parseUploadSize(s string) (int64, error)` — package `main` in `cmd/apicli`.

Why it lives at the flag layer, not in `internal/apicli`: it parses a CLI flag
value, and nothing inside `internal/apicli` needs it — `EncodeForm` already
takes a plain `int64`.

- [ ] **Step 1: Write the failing test**

Append to `cmd/apicli/commands_test.go`:

```go
func TestParseUploadSize(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int64
	}{
		{"1024", 1024},
		{"512MB", 512 << 20},
		{"512mb", 512 << 20},
		{" 2GB ", 2 << 30},
		{"64KB", 64 << 10},
	} {
		got, err := parseUploadSize(tc.in)
		if err != nil {
			t.Errorf("parseUploadSize(%q) errored: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseUploadSize(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
	for _, bad := range []string{"", "0", "-5", "abc", "12PB", "9223372036854775807GB"} {
		if _, err := parseUploadSize(bad); err == nil {
			t.Errorf("expected error for --max-upload %q, got nil", bad)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/whoog/Dev/aidev-clis-form-upload && go test ./cmd/apicli/ -run TestParseUploadSize -v`

Expected: FAIL — `undefined: parseUploadSize`.

- [ ] **Step 3: Write minimal implementation**

Add `"strconv"` to the import block of `cmd/apicli/commands.go`, then append:

```go
// parseUploadSize accepts a byte count with an optional KB/MB/GB suffix
// (case-insensitive); a bare number is bytes. The cap it feeds is a RAM
// guardrail, not a protocol limit — upload bytes never enter the JSON envelope.
func parseUploadSize(s string) (int64, error) {
	invalid := errs.Config("MAX_UPLOAD_INVALID",
		"--max-upload must be a positive size like 512MB, 2GB, or a plain byte count: "+s)
	t := strings.TrimSpace(strings.ToUpper(s))
	mult := int64(1)
	switch {
	case strings.HasSuffix(t, "GB"):
		mult, t = 1<<30, strings.TrimSuffix(t, "GB")
	case strings.HasSuffix(t, "MB"):
		mult, t = 1<<20, strings.TrimSuffix(t, "MB")
	case strings.HasSuffix(t, "KB"):
		mult, t = 1<<10, strings.TrimSuffix(t, "KB")
	}
	n, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
	if err != nil || n <= 0 || n > (1<<62)/mult {
		return 0, invalid
	}
	return n * mult, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/whoog/Dev/aidev-clis-form-upload && go test ./cmd/apicli/ -run TestParseUploadSize -v`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/whoog/Dev/aidev-clis-form-upload
git add cmd/apicli/commands.go cmd/apicli/commands_test.go
git commit -m "Add --max-upload size parser"
```

---

### Task 4: Wire the encoded body through `CallRequest`, `DoRequest`, and `ToCurl`

**Files:**
- Modify: `internal/apicli/http.go:27-39` (`CallRequest` struct), `internal/apicli/http.go:101-105` (after the `-H` loop in `DoRequest`), `internal/apicli/http.go:243-268` (`ToCurl`)
- Test: `internal/apicli/curl_test.go` (append), `internal/apicli/http_test.go` (append)

**Interfaces:**
- Consumes: `FormPart` (Task 1), `formArgString` (Task 2).
- Produces: two new `CallRequest` fields consumed by Task 5 and Task 7:
  - `Form []FormPart` — parsed parts, used only by `ToCurl` and the audit record.
  - `ContentType string` — the `multipart/form-data; boundary=...` value from `EncodeForm`.

- [ ] **Step 1: Write the failing test**

Append to `internal/apicli/curl_test.go`:

```go
func TestToCurlRendersFormParts(t *testing.T) {
	tg := &Target{BaseURL: "https://h.example"}
	parts, err := ParseFormArgs([]string{
		"entranceExitId=11085",
		"images=@/tmp/a.png",
		"images=@/tmp/b.png;type=image/png",
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := ToCurl(tg, &CallRequest{Method: "POST", Path: "/api/upload", Form: parts})
	for _, want := range []string{
		"curl -X POST",
		"-F 'entranceExitId=11085'",
		"-F 'images=@/tmp/a.png'",
		"-F 'images=@/tmp/b.png;type=image/png'",
		"https://h.example/api/upload",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("curl output missing %q in:\n%s", want, got)
		}
	}
	// The boundary is curl's to generate — printing ours would make the
	// command non-reproducible.
	if strings.Contains(got, "boundary") {
		t.Errorf("curl output must not pin a boundary:\n%s", got)
	}
}
```

Append to `internal/apicli/http_test.go`:

```go
// TestDoRequestFormContentTypeWinsOverHeader is defense in depth: the command
// layer rejects -F together with -H 'Content-Type', but if one ever reaches
// DoRequest the computed boundary must still survive. Losing it reproduces the
// exact "no multipart boundary was found" failure this flag exists to remove.
func TestDoRequestFormContentTypeWinsOverHeader(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Content-Type")
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()

	tg := &Target{BaseURL: srv.URL, Auth: Auth{Kind: "none"}}
	req := &CallRequest{
		Method:      "POST",
		Path:        "/upload",
		Headers:     []string{"Content-Type: multipart/form-data"},
		Body:        []byte("irrelevant"),
		ContentType: "multipart/form-data; boundary=XYZ",
	}
	if _, err := DoRequest(tg, req, Session{}); err != nil {
		t.Fatalf("DoRequest: %v", err)
	}
	if seen != "multipart/form-data; boundary=XYZ" {
		t.Fatalf("server saw Content-Type %q, want the computed one with the boundary", seen)
	}
}
```

If `internal/apicli/http_test.go` does not already import `net/http` and
`net/http/httptest`, add them.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/whoog/Dev/aidev-clis-form-upload && go test ./internal/apicli/ -run 'TestToCurlRendersFormParts|TestDoRequestFormContentType' -v`

Expected: FAIL — `unknown field Form in struct literal`, `unknown field ContentType in struct literal`.

- [ ] **Step 3: Write minimal implementation**

In `internal/apicli/http.go`, extend `CallRequest` (after the `Body` field):

```go
	Body    []byte
	Timeout time.Duration

	// Form holds the parsed -F parts. Body already carries their encoding —
	// Form exists only so ToCurl and the audit record can describe the request
	// without re-parsing. ContentType is the multipart/form-data value
	// (boundary included) that EncodeForm computed alongside Body.
	Form        []FormPart
	ContentType string
```

In `DoRequest`, immediately **after** the per-call `-H` loop (currently
`http.go:101-105`):

```go
	// AFTER the -H loop on purpose. -F computes the boundary, and a per-call
	// -H Content-Type would otherwise clobber it — which is precisely the
	// "no multipart boundary was found" failure this flag removes. The command
	// layer rejects that combination outright; this is the second line.
	if req.ContentType != "" {
		hreq.Header.Set("Content-Type", req.ContentType)
	}
```

In `ToCurl`, insert the form loop just before the existing `-d` block:

```go
	for _, p := range req.Form {
		b.WriteString(" \\\n  -F '" + formArgString(p) + "'")
	}
	if len(req.Body) > 0 {
		b.WriteString(" \\\n  -d '" + string(req.Body) + "'")
	}
```

`-F` and `-d` are mutually exclusive at the command layer, and the `--curl`
preview never encodes, so `Body` is empty whenever `Form` is set — no guard is
needed between the two.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/whoog/Dev/aidev-clis-form-upload && go test ./internal/apicli/ -v`

Expected: PASS — the whole package, including the pre-existing `TestToCurl` and `TestToCurlRedactsLiveSecrets`.

- [ ] **Step 5: Commit**

```bash
cd /Users/whoog/Dev/aidev-clis-form-upload
git add internal/apicli/http.go internal/apicli/curl_test.go internal/apicli/http_test.go
git commit -m "Carry the multipart body and Content-Type through CallRequest"
```

---

### Task 5: Add the `-F` / `--max-upload` flags and make upload work end to end

**Files:**
- Modify: `cmd/apicli/commands.go` — `callCmd` (var block, `RunE`, flag registration)
- Test: `cmd/apicli/commands_test.go` (append)

**Interfaces:**
- Consumes: `apicli.ParseFormArgs`, `apicli.EncodeForm`, `apicli.FormPart` (Tasks 1–2); `parseUploadSize` (Task 3); `CallRequest.Form` / `.ContentType` (Task 4).
- Produces: working `-F` uploads. Task 6 adds the conflict rejections, Task 7 the audit record.

- [ ] **Step 1: Write the failing test**

Append to `cmd/apicli/commands_test.go`:

```go
// TestCallFormUploadIsBinarySafe is the end-to-end regression for the two
// shell-variable failure modes: command substitution strips trailing newlines,
// and shell variables truncate at NUL. Going file -> socket must preserve bytes.
func TestCallFormUploadIsBinarySafe(t *testing.T) {
	raw := []byte{0x89, 'P', 'N', 'G', 0x00, 0x00, '\r', '\n', 0xFF, '\n', '\n'}

	var gotMethod string
	var gotNames []string
	var gotSums [][32]byte
	var gotField string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("server could not parse multipart: %v", err)
			w.WriteHeader(400)
			return
		}
		gotField = r.FormValue("entranceExitId")
		for _, fh := range r.MultipartForm.File["images"] {
			gotNames = append(gotNames, fh.Filename)
			f, err := fh.Open()
			if err != nil {
				t.Errorf("open uploaded file: %v", err)
				continue
			}
			b, _ := io.ReadAll(f)
			_ = f.Close()
			gotSums = append(gotSums, sha256.Sum256(b))
		}
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	writeApicliYAML(t, home, srv.URL)

	a := filepath.Join(home, "a.png")
	b := filepath.Join(home, "b.png")
	if err := os.WriteFile(a, raw, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.WriteFile(b, raw, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	out := runCLI(t, "call", "shop", "/api/upload", "--base-url", srv.URL,
		"-F", "entranceExitId=11085",
		"-F", "images=@"+a,
		"-F", "images=@"+b)

	// -F implies POST, matching curl — otherwise this would be a body-bearing GET.
	if gotMethod != "POST" {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotField != "11085" {
		t.Errorf("plain field = %q, want 11085", gotField)
	}
	// Repeated -F with one name binds as a list, in order.
	if len(gotNames) != 2 || gotNames[0] != "a.png" || gotNames[1] != "b.png" {
		t.Fatalf("uploaded filenames = %v, want [a.png b.png]", gotNames)
	}
	want := sha256.Sum256(raw)
	for i, sum := range gotSums {
		if sum != want {
			t.Errorf("file %d altered in transit: got %x want %x", i, sum, want)
		}
	}
	if !bytes.Contains(out, []byte(`"ok":true`)) {
		t.Errorf("expected ok envelope, got: %s", out)
	}
}

// TestCallFormExplicitMethodWins proves -X still beats the POST default.
func TestCallFormExplicitMethodWins(t *testing.T) {
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	writeApicliYAML(t, home, srv.URL)

	runCLI(t, "call", "shop", "/api/upload", "--base-url", srv.URL,
		"-X", "PUT", "-F", "a=1")
	if gotMethod != "PUT" {
		t.Errorf("method = %q, want PUT — explicit -X must beat the -F POST default", gotMethod)
	}
}

func TestCallFormRejectsOverCap(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	writeApicliYAML(t, home, "http://unused.example")
	f := filepath.Join(home, "big.bin")
	if err := os.WriteFile(f, make([]byte, 4096), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	// Validation runs before any network call, so no test server is needed.
	out := runCLI(t, "call", "shop", "/api/upload", "-F", "f=@"+f, "--max-upload", "1024")
	if !bytes.Contains(out, []byte("FORM_TOO_LARGE")) {
		t.Fatalf("expected FORM_TOO_LARGE, got: %s", out)
	}
	if !bytes.Contains(out, []byte("--max-upload")) {
		t.Errorf("error should name --max-upload so the cap is adjustable, got: %s", out)
	}
}
```

Ensure `cmd/apicli/commands_test.go` imports `crypto/sha256` and `io`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/whoog/Dev/aidev-clis-form-upload && go test ./cmd/apicli/ -run 'TestCallForm' -v`

Expected: FAIL — `unknown flag: -F`.

- [ ] **Step 3: Write minimal implementation**

In `cmd/apicli/commands.go`, extend `callCmd`'s var block:

```go
	var method, data, output string
	var outputFile, headersFile, maxUpload string
	var headers, form []string
```

Inside `RunE`, immediately after `method = strings.ToUpper(method)`, insert:

```go
				// curl semantics: -F implies POST unless -X was given
				// explicitly. Without this the -X default of GET would send a
				// body-bearing GET, which no server treats as an upload.
				if len(form) > 0 && !cmd.Flags().Changed("request") {
					method = http.MethodPost
				}
				var formParts []apicli.FormPart
				var formBody []byte
				var formContentType string
				if len(form) > 0 {
					formParts, err = apicli.ParseFormArgs(form)
					if err != nil {
						beginAudit(tg.App, tg.Env, false, nil).Finish(err, nil)
						return err
					}
				}
```

Add `"net/http"` to the file's import block.

Replace the `--curl` preview block so the parts are shown (it must **not**
encode — a preview never reads files):

```go
				if curl {
					// Preview only — never hits the backend, so not side-effecting.
					beginAudit(tg.App, tg.Env, false, nil).Finish(nil, nil)
					_, _ = os.Stdout.WriteString(apicli.ToCurl(tg, &apicli.CallRequest{
						Method: method, Path: path, Headers: headers,
						Body: []byte(data), Form: formParts,
					}) + "\n")
					return nil
				}
```

Then, immediately before the `req := &apicli.CallRequest{...}` literal, encode
once:

```go
				body := []byte(data)
				if len(formParts) > 0 {
					limit, perr := parseUploadSize(maxUpload)
					if perr != nil {
						beginAudit(tg.App, tg.Env, false, nil).Finish(perr, nil)
						return perr
					}
					// Encoded ONCE, here. Call may resend this exact []byte
					// after an auto-relogin; re-encoding per attempt would
					// re-read the files and change the boundary.
					body, formContentType, err = apicli.EncodeForm(formParts, limit)
					if err != nil {
						beginAudit(tg.App, tg.Env, false, nil).Finish(err, nil)
						return err
					}
				}
```

And update the request literal to use them:

```go
				req := &apicli.CallRequest{
					Method: method, Path: path, Headers: headers,
					Body: body, Form: formParts, ContentType: formContentType,
					Timeout:    timeout,
					OutputFile: outputFile, HeadersFile: headersFile,
					AllowCrossOrigin: allowCrossOrigin,
				}
```

Delete the now-unused `formBody` declaration from the block added earlier —
`body` carries the encoding.

Finally, register the flags beside the existing ones:

```go
	c.Flags().StringArrayVarP(&form, "form", "F", nil,
		"multipart field, repeatable: name=value or name=@file[;type=..][;filename=..] (implies POST)")
	c.Flags().StringVar(&maxUpload, "max-upload", "512MB",
		"cap on total upload bytes, e.g. 512MB or 2GB")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/whoog/Dev/aidev-clis-form-upload && go test ./cmd/apicli/ -v -run 'TestCallForm|TestParseUploadSize'`

Expected: PASS — all four tests.

- [ ] **Step 5: Commit**

```bash
cd /Users/whoog/Dev/aidev-clis-form-upload
git add cmd/apicli/commands.go cmd/apicli/commands_test.go
git commit -m "Add apicli call -F/--form for multipart uploads"
```

---

### Task 6: Reject `-F` combined with `-d` or an explicit `Content-Type`

**Files:**
- Modify: `cmd/apicli/commands.go` — `callCmd` `RunE`, plus a `hasContentTypeHeader` helper beside `headerMap`
- Test: `cmd/apicli/commands_test.go` (append)

**Interfaces:**
- Consumes: the `form` / `data` / `headers` locals from Task 5.
- Produces: `func hasContentTypeHeader(headers []string) bool`.

Why this is its own gate: it is the difference between reproducing the original
`no multipart boundary was found` failure and telling the caller why, locally,
before a request is sent.

- [ ] **Step 1: Write the failing test**

Append to `cmd/apicli/commands_test.go`:

```go
func TestCallFormRejectsConflictingFlags(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	writeApicliYAML(t, home, "http://unused.example")

	// -F builds the body itself; -d would be silently discarded.
	out := runCLI(t, "call", "shop", "/api/upload", "-F", "a=1", "-d", `{"x":1}`)
	if !bytes.Contains(out, []byte("REQUEST_INVALID")) {
		t.Errorf("expected REQUEST_INVALID for -F with -d, got: %s", out)
	}

	// A hand-written Content-Type has no boundary and would break the upload —
	// this is the exact failure -F exists to remove, so reject it loudly.
	out = runCLI(t, "call", "shop", "/api/upload",
		"-F", "a=1", "-H", "content-type: multipart/form-data")
	if !bytes.Contains(out, []byte("REQUEST_INVALID")) {
		t.Errorf("expected REQUEST_INVALID for -F with -H Content-Type, got: %s", out)
	}
	if !bytes.Contains(out, []byte("boundary")) {
		t.Errorf("error should explain that -F computes the boundary, got: %s", out)
	}

	// An unrelated -H is fine.
	out = runCLI(t, "call", "shop", "/api/upload", "-F", "a=1", "-H", "X-Trace: t1")
	if bytes.Contains(out, []byte("REQUEST_INVALID")) {
		t.Errorf("an unrelated -H must not be rejected, got: %s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/whoog/Dev/aidev-clis-form-upload && go test ./cmd/apicli/ -run TestCallFormRejectsConflictingFlags -v`

Expected: FAIL — no `REQUEST_INVALID` in the output; the first two calls attempt a real request to `unused.example` instead.

- [ ] **Step 3: Write minimal implementation**

In `cmd/apicli/commands.go`, inside the `if len(form) > 0 {` block added in
Task 5, **before** the `ParseFormArgs` call:

```go
				if len(form) > 0 {
					if data != "" {
						e := errs.Config("REQUEST_INVALID",
							"-F and -d are mutually exclusive: -F builds the request body itself")
						beginAudit(tg.App, tg.Env, false, nil).Finish(e, nil)
						return e
					}
					if hasContentTypeHeader(headers) {
						e := errs.Config("REQUEST_INVALID",
							"-F computes Content-Type and the multipart boundary itself; drop the -H 'Content-Type: ...'")
						beginAudit(tg.App, tg.Env, false, nil).Finish(e, nil)
						return e
					}
					formParts, err = apicli.ParseFormArgs(form)
					...
				}
```

And append beside `headerMap`:

```go
// hasContentTypeHeader reports whether a per-call -H sets Content-Type. Matching
// is case-insensitive because header names are.
func hasContentTypeHeader(headers []string) bool {
	for _, h := range headers {
		if k, _, ok := strings.Cut(h, ":"); ok &&
			strings.EqualFold(strings.TrimSpace(k), "Content-Type") {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/whoog/Dev/aidev-clis-form-upload && go test ./cmd/apicli/ -v`

Expected: PASS — the whole package.

- [ ] **Step 5: Commit**

```bash
cd /Users/whoog/Dev/aidev-clis-form-upload
git add cmd/apicli/commands.go cmd/apicli/commands_test.go
git commit -m "Reject -F combined with -d or an explicit Content-Type"
```

---

### Task 7: Record the multipart shape in the audit trail, never its content

**Files:**
- Modify: `cmd/apicli/commands.go` — the `reqMap` construction in `callCmd`, plus a `formAudit` helper beside `headerMap`
- Test: `cmd/apicli/commands_test.go` (append)

**Interfaces:**
- Consumes: `apicli.FormPart` with `Bytes` populated by `EncodeForm` (Task 2).
- Produces: `func formAudit(parts []apicli.FormPart) []map[string]any`.

- [ ] **Step 1: Write the failing test**

Append to `cmd/apicli/commands_test.go`. It reuses the audit-reading pattern
from `cmd/jcli/audit_test.go`:

```go
// readApicliAuditLines returns every audit JSONL line under $AIDEV_CLIS_HOME/audit/.
func readApicliAuditLines(t *testing.T, home string) []map[string]any {
	t.Helper()
	dir := filepath.Join(home, "audit")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read audit dir: %v", err)
	}
	var lines []map[string]any
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		for _, ln := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if ln == "" {
				continue
			}
			var m map[string]any
			if err := json.Unmarshal([]byte(ln), &m); err != nil {
				t.Fatalf("bad audit line %q: %v", ln, err)
			}
			lines = append(lines, m)
		}
	}
	return lines
}

// TestCallFormAuditRecordsShapeNotContent: the audit trail must let an operator
// see WHAT was uploaded (field names, paths, sizes) without recording the
// values or the bytes. -d bodies are not audited either; this keeps parity and
// keeps PII off disk.
func TestCallFormAuditRecordsShapeNotContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	writeApicliYAML(t, home, srv.URL)

	img := filepath.Join(home, "a.png")
	if err := os.WriteFile(img, []byte("ABCDE"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	runCLI(t, "call", "shop", "/api/upload", "--base-url", srv.URL,
		"-F", "idCardNo=110101199001011234",
		"-F", "images=@"+img)

	var form []any
	for _, ln := range readApicliAuditLines(t, home) {
		req, ok := ln["request"].(map[string]any)
		if !ok {
			continue
		}
		if f, ok := req["form"].([]any); ok {
			form = f
		}
	}
	if len(form) != 2 {
		t.Fatalf("audit should record 2 form parts, got %d: %+v", len(form), form)
	}
	plain := form[0].(map[string]any)
	if plain["name"] != "idCardNo" {
		t.Errorf("plain part name = %v, want idCardNo", plain["name"])
	}
	if _, leaked := plain["value"]; leaked {
		t.Error("audit must never record a form field VALUE")
	}
	file := form[1].(map[string]any)
	if file["name"] != "images" || file["file"] != img {
		t.Errorf("file part wrong: %+v", file)
	}
	if bytesLogged, _ := file["bytes"].(float64); bytesLogged != 5 {
		t.Errorf("file part bytes = %v, want 5", file["bytes"])
	}

	// Belt and braces: the value must not appear anywhere in the audit files.
	entries, _ := os.ReadDir(filepath.Join(home, "audit"))
	for _, e := range entries {
		b, _ := os.ReadFile(filepath.Join(home, "audit", e.Name()))
		if bytes.Contains(b, []byte("110101199001011234")) {
			t.Fatalf("form field value leaked into %s", e.Name())
		}
		if bytes.Contains(b, []byte("ABCDE")) {
			t.Fatalf("file content leaked into %s", e.Name())
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/whoog/Dev/aidev-clis-form-upload && go test ./cmd/apicli/ -run TestCallFormAuditRecordsShapeNotContent -v`

Expected: FAIL — `audit should record 2 form parts, got 0`.

- [ ] **Step 3: Write minimal implementation**

In `cmd/apicli/commands.go`, extend the `reqMap` construction (right after the
existing `if h := headerMap(headers); len(h) > 0 { ... }` block):

```go
				if len(formParts) > 0 {
					reqMap["form"] = formAudit(formParts)
				}
```

`Bytes` is already populated at this point: in the current file the `req`
literal precedes `reqMap`, and Task 5 places the encode block immediately
before that literal. No reordering is needed — just verify the encode block is
above `reqMap` before adding this.

Append beside `headerMap`:

```go
// formAudit records the SHAPE of a multipart body — field names, file paths and
// sizes. Field VALUES and file content never enter the audit, matching -d
// bodies, which are not audited either.
func formAudit(parts []apicli.FormPart) []map[string]any {
	out := make([]map[string]any, 0, len(parts))
	for _, p := range parts {
		e := map[string]any{"name": p.Name}
		if p.File != "" {
			e["file"] = p.File
			e["bytes"] = p.Bytes
		}
		out = append(out, e)
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/whoog/Dev/aidev-clis-form-upload && go test ./cmd/apicli/ -v`

Expected: PASS — the whole package.

- [ ] **Step 5: Commit**

```bash
cd /Users/whoog/Dev/aidev-clis-form-upload
git add cmd/apicli/commands.go cmd/apicli/commands_test.go
git commit -m "Audit the multipart shape without recording values or content"
```

---

### Task 8: Document upload as the other half of binary safety

**Files:**
- Modify: `docs/cli-apicli.md` (the flag table around line 72, plus a new subsection beside the `--output-file` discussion around line 86)
- Modify: `skills/aidev-apicli/SKILL.md` (the flag table around line 113, plus the body-passthrough note around line 124)

**Interfaces:**
- Consumes: the finished flag surface from Tasks 5–6.
- Produces: nothing code-facing.

`-h` remains the truth source for flags. Both files already carry a flag table,
so add one row to each and keep the prose about the durable model, not an
enumeration.

- [ ] **Step 1: Update `docs/cli-apicli.md`**

Add to the flag table, directly under the `--data` row:

```markdown
| `--form` | `-F` | — | multipart field, repeatable (implies POST) |
| `--max-upload` | — | `512MB` | cap on total upload bytes |
```

Then, next to the existing `--output-file` section, add:

```markdown
### Binary-safe in both directions

`--output-file` covers downloads; `-F` covers uploads. They exist for the same
reason: bytes must not round-trip through a shell variable or the JSON envelope.

`-d` is a raw string. Routing a file through it is not merely awkward, it is
unsound — shell command substitution strips trailing newlines (corrupting a
multipart body's byte-sensitive closing delimiter) and shell variables truncate
at the NUL bytes present in real PNG/JPEG content. `-F` reads the file itself:

```sh
apicli call shop /api/entrance/upload \
  -F 'entranceExitId=11085' \
  -F 'images=@/tmp/a.png' \
  -F 'images=@/tmp/b.png;type=image/png'
```

Repeating a field name sends repeated parts in order, which is what a Spring
`List<MultipartFile>` parameter binds to. apicli computes the boundary,
`Content-Type`, and `Content-Length`; passing your own
`-H 'Content-Type: multipart/form-data'` is rejected rather than silently
overriding the boundary.

`;type=` sets the part's content type (otherwise inferred from the extension)
and `;filename=` overrides the transmitted name. Both are parsed only on the
`@file` form — for a plain field, everything after `=` is the literal value.

The body is buffered in memory so an expired session can be replayed after an
automatic re-login. `--max-upload` bounds that buffer; it is a memory guardrail,
not a protocol limit.
```

- [ ] **Step 2: Update `skills/aidev-apicli/SKILL.md`**

Add the same two rows to its flag table, and beside the existing body-passthrough
note add the agent-facing version:

```markdown
- **`-F 'name=value'` / `-F 'name=@/path/file.png'`** — multipart upload,
  repeatable; implies POST. Reads the file from disk, so binary is safe.
  **Never** build a multipart body by hand and pass it via `-d`: command
  substitution strips trailing newlines and shell variables truncate at NUL,
  so binary files corrupt silently. Repeat the same name for a list of files
  (`-F 'images=@a.png' -F 'images=@b.png'`). Add `;type=image/png` to set the
  part's content type, `;filename=x.png` to rename it. Do not pass
  `-H 'Content-Type: ...'` alongside `-F` — apicli computes the boundary and
  will reject the combination. `--max-upload` (default `512MB`) bounds the
  in-memory buffer.
```

Also update the skill's `description:` frontmatter, which currently ends
`...and file downloads (--output-file).` — change that to
`...and binary-safe file transfer in both directions (-F upload, --output-file download).`

- [ ] **Step 3: Verify the docs match reality**

Run: `cd /Users/whoog/Dev/aidev-clis-form-upload && go run ./cmd/apicli call --help`

Expected: the `-F, --form` and `--max-upload` entries appear with the help text
from Task 5. Confirm every flag named in the two docs exists in this output —
`-h` is the truth source, so any drift is a docs bug.

- [ ] **Step 4: Run the full pre-commit gate**

Run: `cd /Users/whoog/Dev/aidev-clis-form-upload && make check`

Expected: PASS — gofmt, vet, build, the adapter-isolation guards, the crossbuild
of all three GOOS, and the full test suite.

- [ ] **Step 5: Commit**

```bash
cd /Users/whoog/Dev/aidev-clis-form-upload
git add docs/cli-apicli.md skills/aidev-apicli/SKILL.md
git commit -m "Document -F as the upload half of binary safety"
```

---

## Self-Review

**Spec coverage** — every spec section maps to a task:

| Spec section | Task |
|---|---|
| Flag surface table (`name=value`, `@file`, `;type=`, `;filename=`, repeated names) | 1, 2 |
| `-F` implies POST | 5 |
| Deviation: modifiers only on the `@` form | 1 (asserted by `note=a;b=c`) |
| Non-goals (`<file`, stdin, `--form-string`, `--data-binary`, tcli) | none — deliberately unimplemented |
| Replayable body constraint | 2 (buffered `[]byte`), 5 (encode once) |
| `ParseFormArgs` / `EncodeForm` signatures | 1, 2 |
| `FormPart` with `Bytes` | 1 (fields), 2 (populated) |
| `CallRequest.Form` / `.ContentType` | 4 |
| `Content-Type` set after the `-H` loop | 4 |
| Size cap, `--max-upload`, suffix parsing, stat-before-read | 2, 3, 5 |
| All seven error codes | 1 (`FORM_ARG_INVALID`), 2 (`FORM_FILE_UNREADABLE`, `FORM_TOO_LARGE`, `FORM_ENCODE_FAILED`), 3 (`MAX_UPLOAD_INVALID`), 6 (both `REQUEST_INVALID`) |
| `--curl` renders `-F`, no boundary | 4 |
| Audit shape only | 7 |
| Binary-safety regression | 2 (unit), 5 (end-to-end) |
| Cross-platform (`filepath`, `t.TempDir`) | 1, 2, 5 |
| Docs | 8 |

**Placeholder scan:** no TBD/TODO, no "add error handling", no "similar to Task N". Every code step carries complete code.

**Type consistency:** `FormPart` fields (`Name`, `Value`, `File`, `Filename`, `ContentType`, `Bytes`) are declared in Task 1 and used unchanged in Tasks 2, 4, and 7. `ParseFormArgs([]string) ([]FormPart, error)` and `EncodeForm([]FormPart, int64) ([]byte, string, error)` are used with those exact signatures in Task 5. `parseUploadSize(string) (int64, error)` from Task 3 feeds `EncodeForm`'s `maxBytes` in Task 5. `formArgString(FormPart) string` from Task 2 is called by `ToCurl` in Task 4 — same package, so the unexported name resolves.

**One ordering dependency, verified against the current file:** `reqMap` reads `FormPart.Bytes`, which `EncodeForm` populates. In `callCmd` today the `req` literal precedes `reqMap`, and Task 5 places the encode block immediately before that literal — so `Bytes` is populated by the time Task 7's `reqMap` addition runs. No reordering required; Task 7 Step 3 says so explicitly.
