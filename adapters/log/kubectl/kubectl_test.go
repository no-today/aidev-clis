package kubectl

import (
	"context"
	"os"
	"path/filepath"
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

func env(t *testing.T) config.Target {
	t.Helper()
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	dir := filepath.Join(home, "credentials")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "kube.cfg"), []byte("apiVersion: v1"), 0o600)
	return config.Target{Name: "uat", Adapter: "kubectl", Raw: map[string]any{
		"kubeconfig_credential": "kube.cfg", "namespace": "uat",
	}}
}

func TestKubectl_LogsBuildsArgvAndInjectsKubeconfig(t *testing.T) {
	fr := &fakeRunner{lines: []string{"l1", "l2"}}
	a := adapter{runner: fr}
	out := &capOut{}
	err := a.Run(context.Background(), logcli.Input{Target: env(t), Args: []string{"logs", "-l", "app=x"}}, out)
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Join(fr.gotSpec.Argv, " ")
	if !strings.Contains(argv, "kubectl logs -l app=x") || !strings.Contains(argv, "--namespace uat") {
		t.Fatalf("argv=%q", argv)
	}
	if len(fr.gotSpec.Env) == 0 || !strings.HasPrefix(fr.gotSpec.Env[0], "KUBECONFIG=") {
		t.Fatalf("KUBECONFIG not injected: %v", fr.gotSpec.Env)
	}
	if len(out.batched) != 2 {
		t.Fatalf("want 2 batched, got %d", len(out.batched))
	}
}

func TestKubectl_LogsInjectsDefaultTail(t *testing.T) {
	fr := &fakeRunner{lines: []string{"l1"}}
	a := adapter{runner: fr}
	if err := a.Run(context.Background(), logcli.Input{Target: env(t), Args: []string{"logs", "-l", "app=x"}}, &capOut{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(fr.gotSpec.Argv, " "), "--tail=200") {
		t.Fatalf("no --tail supplied → default must be injected: %v", fr.gotSpec.Argv)
	}
}

func TestKubectl_LogsDoesNotOverrideUserTail(t *testing.T) {
	fr := &fakeRunner{lines: []string{"l1"}}
	a := adapter{runner: fr}
	if err := a.Run(context.Background(), logcli.Input{Target: env(t), Args: []string{"logs", "--tail=5", "p1"}}, &capOut{}); err != nil {
		t.Fatal(err)
	}
	argv := strings.Join(fr.gotSpec.Argv, " ")
	if strings.Contains(argv, "--tail=200") || !strings.Contains(argv, "--tail=5") {
		t.Fatalf("user --tail must be preserved without injection: %v", fr.gotSpec.Argv)
	}
}

func TestKubectl_LogsFollowStillBoundsBacklog(t *testing.T) {
	fr := &fakeRunner{lines: []string{"x"}}
	a := adapter{runner: fr}
	if err := a.Run(context.Background(), logcli.Input{Target: env(t), Args: []string{"logs", "-f"}}, &capOut{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(fr.gotSpec.Argv, " "), "--tail=200") {
		t.Fatalf("follow without --tail must still bound the backlog: %v", fr.gotSpec.Argv)
	}
}

func TestKubectl_LsListsDeployments(t *testing.T) {
	fr := &fakeRunner{lines: []string{"svc-a\tsvc-a", "svc-b\t"}}
	a := adapter{runner: fr}
	out := &capOut{}
	if err := a.Run(context.Background(), logcli.Input{Target: env(t), Args: []string{"ls"}}, out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(fr.gotSpec.Argv, " "), "get deployments") {
		t.Fatalf("ls must run get deployments: %v", fr.gotSpec.Argv)
	}
	if len(fr.gotSpec.Env) == 0 || !strings.HasPrefix(fr.gotSpec.Env[0], "KUBECONFIG=") {
		t.Fatalf("KUBECONFIG must be injected on the ls path too: %v", fr.gotSpec.Env)
	}
	if len(out.batched) != 2 || out.batched[0].(map[string]any)["name"] != "svc-a" {
		t.Fatalf("bad ls output: %v", out.batched)
	}
}

func TestKubectl_RejectsBadVerbAndDangerousFlag(t *testing.T) {
	a := adapter{runner: &fakeRunner{}}
	if err := a.Run(context.Background(), logcli.Input{Target: env(t), Args: []string{"delete", "pod"}}, &capOut{}); err == nil {
		t.Fatal("delete must be rejected")
	}
	if err := a.Run(context.Background(), logcli.Input{Target: env(t), Args: []string{"logs", "--kubeconfig", "/evil"}}, &capOut{}); err == nil {
		t.Fatal("--kubeconfig override must be rejected")
	}
	for _, bad := range []string{"--as-group", "--as-uid", "--username", "--password", "--client-certificate", "--client-key", "--insecure-skip-tls-verify"} {
		if err := a.Run(context.Background(), logcli.Input{Target: env(t), Args: []string{"logs", bad, "x"}}, &capOut{}); err == nil {
			t.Fatalf("identity/TLS override %q must be rejected", bad)
		}
	}
}

func TestKubectl_FollowStreams(t *testing.T) {
	fr := &fakeRunner{lines: []string{"x"}}
	a := adapter{runner: fr}
	out := &capOut{}
	if err := a.Run(context.Background(), logcli.Input{Target: env(t), Args: []string{"logs", "-f"}}, out); err != nil {
		t.Fatal(err)
	}
	if !out.stream {
		t.Fatal("-f must use Stream")
	}
}

func TestKubectl_Doctor(t *testing.T) {
	a := adapter{runner: &fakeRunner{}}
	out := &capOut{}
	if err := a.Run(context.Background(), logcli.Input{Target: env(t), Args: []string{"doctor"}}, out); err != nil {
		t.Fatal(err)
	}
	if len(out.batched) != 2 || out.batched[1].(logcli.Check).Name != "connect" || !out.batched[1].(logcli.Check).OK {
		t.Fatalf("checks: %+v", out.batched)
	}
}
