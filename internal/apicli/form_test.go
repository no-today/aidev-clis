package apicli

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
		"noequals",          // no "="
		"=value",            // empty name
		"f=@",               // empty file path
		"f=@/tmp/x;bogus=1", // unknown modifier
		"f=@/tmp/x;novalue", // modifier without "="
	} {
		if _, err := ParseFormArgs([]string{arg}); err == nil {
			t.Errorf("expected error for -F %q, got nil", arg)
		}
	}
}

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
