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
