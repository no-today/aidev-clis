package sshfile

import (
	"context"
	"strings"
	"testing"

	"github.com/no-today/aidev-clis/internal/core/config"
	"github.com/no-today/aidev-clis/internal/core/exec"
	"github.com/no-today/aidev-clis/internal/logcli"
)

type fakeRunner struct {
	gotSpec exec.Spec
	lines   []string
}

func (f *fakeRunner) Run(_ context.Context, s exec.Spec, onLine func(string) error) error {
	f.gotSpec = s
	for _, l := range f.lines {
		if err := onLine(l); err != nil {
			return err
		}
	}
	return nil
}

type capOut struct {
	batched []any
	stream  bool
}

func (c *capOut) Batch(r []any, _ ...string) error { c.batched = r; return nil }
func (c *capOut) Stream() logcli.Streamer          { c.stream = true; return capStream{} }

type capStream struct{}

func (capStream) Record(any) error { return nil }

func envWith(files ...any) config.Target {
	return config.Target{Name: "box", Adapter: "ssh-file", Raw: map[string]any{
		"host": "h", "user": "u", "files": files,
	}}
}

func TestSSHFile_LogsTailsConfiguredFiles(t *testing.T) {
	fr := &fakeRunner{lines: []string{"x", "y"}}
	a := adapter{runner: fr}
	out := &capOut{}
	err := a.Run(context.Background(), logcli.Input{Target: envWith("/srv/app.log"), Args: []string{"logs", "--tail", "50"}}, out)
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Join(fr.gotSpec.Argv, " ")
	if !strings.Contains(argv, "tail -n 50") || !strings.Contains(argv, "/srv/app.log") {
		t.Fatalf("argv=%q", argv)
	}
	if len(out.batched) != 2 {
		t.Fatalf("want 2 batched, got %d", len(out.batched))
	}
}

func TestSSHFile_LogsFollowAddsFAndStreams(t *testing.T) {
	fr := &fakeRunner{lines: []string{"x"}}
	a := adapter{runner: fr}
	out := &capOut{}
	if err := a.Run(context.Background(), logcli.Input{Target: envWith("/a.log"), Args: []string{"logs", "-f"}}, out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(fr.gotSpec.Argv, " "), "-f") {
		t.Fatalf("tail -f missing: %v", fr.gotSpec.Argv)
	}
	if !out.stream {
		t.Fatal("-f must stream")
	}
}

func TestSSHFile_LsListsConfiguredFiles(t *testing.T) {
	a := adapter{runner: &fakeRunner{}}
	out := &capOut{}
	if err := a.Run(context.Background(), logcli.Input{Target: envWith("/a.log", "/b.log"), Args: []string{"ls"}}, out); err != nil {
		t.Fatal(err)
	}
	if len(out.batched) != 2 || out.batched[0].(map[string]any)["name"] != "/a.log" {
		t.Fatalf("bad ls: %v", out.batched)
	}
}

func TestSSHFile_NoFilesErrors(t *testing.T) {
	a := adapter{runner: &fakeRunner{}}
	e := config.Target{Adapter: "ssh-file", Raw: map[string]any{"host": "h", "user": "u"}}
	if err := a.Run(context.Background(), logcli.Input{Target: e, Args: []string{"logs"}}, &capOut{}); err == nil {
		t.Fatal("missing files must error")
	}
}

func TestSSHFile_Doctor(t *testing.T) {
	a := adapter{runner: &fakeRunner{}}
	out := &capOut{}
	env := config.Target{Adapter: "ssh-file", Raw: map[string]any{"host": "h", "user": "u", "files": []any{"/a.log"}}}
	if err := a.Run(context.Background(), logcli.Input{Target: env, Args: []string{"doctor"}}, out); err != nil {
		t.Fatal(err)
	}
	if len(out.batched) != 2 || !out.batched[1].(logcli.Check).OK {
		t.Fatalf("checks: %+v", out.batched)
	}
}
