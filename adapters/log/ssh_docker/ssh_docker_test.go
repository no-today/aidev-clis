package sshdocker

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

func env() config.Target {
	return config.Target{Name: "box", Adapter: "ssh-docker", Raw: map[string]any{"host": "h", "user": "u"}}
}

func TestSSHDocker_LogsBuildsDockerLogs(t *testing.T) {
	fr := &fakeRunner{lines: []string{"a"}}
	a := adapter{runner: fr}
	if err := a.Run(context.Background(), logcli.Input{Target: env(), Args: []string{"logs", "--tail", "100", "c1"}}, &capOut{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(fr.gotSpec.Argv, " "), "docker logs --tail 100 c1") {
		t.Fatalf("argv=%v", fr.gotSpec.Argv)
	}
}

func TestSSHDocker_LogsInjectsDefaultTail(t *testing.T) {
	fr := &fakeRunner{lines: []string{"a"}}
	a := adapter{runner: fr}
	if err := a.Run(context.Background(), logcli.Input{Target: env(), Args: []string{"logs", "c1"}}, &capOut{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(fr.gotSpec.Argv, " "), "--tail=200") {
		t.Fatalf("no --tail supplied → default must be injected: %v", fr.gotSpec.Argv)
	}
}

func TestSSHDocker_LogsDoesNotOverrideUserTail(t *testing.T) {
	fr := &fakeRunner{lines: []string{"a"}}
	a := adapter{runner: fr}
	if err := a.Run(context.Background(), logcli.Input{Target: env(), Args: []string{"logs", "--tail", "5", "c1"}}, &capOut{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(fr.gotSpec.Argv, " "), "--tail=200") {
		t.Fatalf("user --tail must be preserved without injection: %v", fr.gotSpec.Argv)
	}
}

func TestSSHDocker_LogsFollowStillBoundsBacklog(t *testing.T) {
	fr := &fakeRunner{lines: []string{"x"}}
	a := adapter{runner: fr}
	if err := a.Run(context.Background(), logcli.Input{Target: env(), Args: []string{"logs", "-f", "c1"}}, &capOut{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(fr.gotSpec.Argv, " "), "--tail=200") {
		t.Fatalf("follow without --tail must still bound the backlog: %v", fr.gotSpec.Argv)
	}
}

func TestSSHDocker_LsRunsPs(t *testing.T) {
	fr := &fakeRunner{lines: []string{"web\tnginx\tUp"}}
	a := adapter{runner: fr}
	out := &capOut{}
	if err := a.Run(context.Background(), logcli.Input{Target: env(), Args: []string{"ls"}}, out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(fr.gotSpec.Argv, " "), "docker ps") {
		t.Fatalf("ls must run docker ps: %v", fr.gotSpec.Argv)
	}
	if len(out.batched) != 1 || out.batched[0].(map[string]any)["name"] != "web" {
		t.Fatalf("bad ls: %v", out.batched)
	}
}

func TestSSHDocker_RejectsBadVerbAndHostFlag(t *testing.T) {
	a := adapter{runner: &fakeRunner{}}
	if err := a.Run(context.Background(), logcli.Input{Target: env(), Args: []string{"rm", "c1"}}, &capOut{}); err == nil {
		t.Fatal("rm must be rejected")
	}
	if err := a.Run(context.Background(), logcli.Input{Target: env(), Args: []string{"logs", "-H", "tcp://evil"}}, &capOut{}); err == nil {
		t.Fatal("-H must be rejected")
	}
}

func TestSSHDocker_FollowStreams(t *testing.T) {
	fr := &fakeRunner{lines: []string{"x"}}
	a := adapter{runner: fr}
	out := &capOut{}
	if err := a.Run(context.Background(), logcli.Input{Target: env(), Args: []string{"logs", "-f", "c1"}}, out); err != nil {
		t.Fatal(err)
	}
	if !out.stream {
		t.Fatal("-f must stream")
	}
}

func TestSSHDocker_Doctor(t *testing.T) {
	a := adapter{runner: &fakeRunner{}}
	out := &capOut{}
	env := config.Target{Adapter: "ssh-docker", Raw: map[string]any{"host": "h", "user": "u"}}
	if err := a.Run(context.Background(), logcli.Input{Target: env, Args: []string{"doctor"}}, out); err != nil {
		t.Fatal(err)
	}
	if len(out.batched) != 2 || !out.batched[1].(logcli.Check).OK {
		t.Fatalf("checks: %+v", out.batched)
	}
}
