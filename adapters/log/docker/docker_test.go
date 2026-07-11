package docker

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

func TestDocker_LogsArgvAndHostInjection(t *testing.T) {
	fr := &fakeRunner{lines: []string{"a"}}
	a := adapter{runner: fr}
	e := config.Target{Adapter: "docker", Raw: map[string]any{"docker_host": "tcp://1.2.3.4:2376"}}
	if err := a.Run(context.Background(), logcli.Input{Target: e, Args: []string{"logs", "--tail", "100", "c1"}}, &capOut{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(fr.gotSpec.Argv, " "), "docker logs --tail 100 c1") {
		t.Fatalf("argv=%v", fr.gotSpec.Argv)
	}
	found := false
	for _, e := range fr.gotSpec.Env {
		if e == "DOCKER_HOST=tcp://1.2.3.4:2376" {
			found = true
		}
	}
	if !found {
		t.Fatalf("DOCKER_HOST not injected: %v", fr.gotSpec.Env)
	}
}

func TestDocker_LogsInjectsDefaultTail(t *testing.T) {
	fr := &fakeRunner{lines: []string{"a"}}
	a := adapter{runner: fr}
	e := config.Target{Adapter: "docker", Raw: map[string]any{}}
	if err := a.Run(context.Background(), logcli.Input{Target: e, Args: []string{"logs", "c1"}}, &capOut{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(fr.gotSpec.Argv, " "), "--tail=200") {
		t.Fatalf("no --tail supplied → default must be injected: %v", fr.gotSpec.Argv)
	}
}

func TestDocker_LogsDoesNotOverrideUserTail(t *testing.T) {
	fr := &fakeRunner{lines: []string{"a"}}
	a := adapter{runner: fr}
	e := config.Target{Adapter: "docker", Raw: map[string]any{}}
	if err := a.Run(context.Background(), logcli.Input{Target: e, Args: []string{"logs", "--tail", "5", "c1"}}, &capOut{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(fr.gotSpec.Argv, " "), "--tail=200") {
		t.Fatalf("user --tail must be preserved without injection: %v", fr.gotSpec.Argv)
	}
}

func TestDocker_LogsFollowStillBoundsBacklog(t *testing.T) {
	fr := &fakeRunner{lines: []string{"x"}}
	a := adapter{runner: fr}
	e := config.Target{Adapter: "docker", Raw: map[string]any{}}
	if err := a.Run(context.Background(), logcli.Input{Target: e, Args: []string{"logs", "-f", "c1"}}, &capOut{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(fr.gotSpec.Argv, " "), "--tail=200") {
		t.Fatalf("follow without --tail must still bound the backlog: %v", fr.gotSpec.Argv)
	}
}

func TestDocker_LsRunsPs(t *testing.T) {
	fr := &fakeRunner{lines: []string{"web\tnginx:1\tUp 2h", "db\tpg:16\tUp 1d"}}
	a := adapter{runner: fr}
	out := &capOut{}
	if err := a.Run(context.Background(), logcli.Input{Target: config.Target{Adapter: "docker", Raw: map[string]any{}}, Args: []string{"ls"}}, out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(fr.gotSpec.Argv, " "), "docker ps") {
		t.Fatalf("ls must run docker ps: %v", fr.gotSpec.Argv)
	}
	if len(out.batched) != 2 || out.batched[0].(map[string]any)["name"] != "web" {
		t.Fatalf("bad ls output: %v", out.batched)
	}
}

func TestDocker_RejectsBadVerbAndHostFlag(t *testing.T) {
	a := adapter{runner: &fakeRunner{}}
	e := config.Target{Adapter: "docker", Raw: map[string]any{}}
	if err := a.Run(context.Background(), logcli.Input{Target: e, Args: []string{"rm", "c1"}}, &capOut{}); err == nil {
		t.Fatal("rm must be rejected")
	}
	if err := a.Run(context.Background(), logcli.Input{Target: e, Args: []string{"logs", "-H", "tcp://evil"}}, &capOut{}); err == nil {
		t.Fatal("-H daemon override must be rejected")
	}
}

func TestDocker_FollowStreams(t *testing.T) {
	fr := &fakeRunner{lines: []string{"x"}}
	a := adapter{runner: fr}
	out := &capOut{}
	e := config.Target{Adapter: "docker", Raw: map[string]any{}}
	if err := a.Run(context.Background(), logcli.Input{Target: e, Args: []string{"logs", "-f", "c1"}}, out); err != nil {
		t.Fatal(err)
	}
	if !out.stream {
		t.Fatal("-f must use Stream")
	}
}

func TestDocker_Doctor(t *testing.T) {
	a := adapter{runner: &fakeRunner{}}
	out := &capOut{}
	if err := a.Run(context.Background(), logcli.Input{Target: config.Target{Adapter: "docker", Raw: map[string]any{}}, Args: []string{"doctor"}}, out); err != nil {
		t.Fatal(err)
	}
	if len(out.batched) != 2 || !out.batched[1].(logcli.Check).OK {
		t.Fatalf("checks: %+v", out.batched)
	}
}
