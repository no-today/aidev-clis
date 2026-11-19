package allow

import "testing"

var pol = Policy{
	Verbs:     []string{"logs", "ls"},
	DenyFlags: []string{"--kubeconfig", "--server", "--token", "--context"},
}

func TestCheck_AllowsVerbAndSafeFlags(t *testing.T) {
	if err := pol.Check([]string{"logs", "-l", "app=x", "--tail", "100", "-f"}); err != nil {
		t.Fatalf("safe flags must pass: %v", err)
	}
	if err := pol.Check([]string{"ls"}); err != nil {
		t.Fatalf("ls must pass: %v", err)
	}
}

func TestCheck_RejectsBadVerb(t *testing.T) {
	if err := pol.Check([]string{"delete", "pod"}); err == nil {
		t.Fatal("non-allowed verb must be rejected")
	}
	if err := pol.Check(nil); err == nil {
		t.Fatal("empty args must be rejected")
	}
}

func TestCheck_RejectsDeniedFlag(t *testing.T) {
	for _, bad := range [][]string{
		{"logs", "--kubeconfig", "/evil"},
		{"logs", "--kubeconfig=/evil"},
		{"logs", "--token", "x"},
	} {
		if err := pol.Check(bad); err == nil {
			t.Fatalf("denied flag must be rejected: %v", bad)
		}
	}
}
