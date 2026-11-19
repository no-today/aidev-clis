package exec

import "testing"

func TestSSHFromConfig_BuildsSSH(t *testing.T) {
	r, err := SSHFromConfig(map[string]any{
		"host": "h", "user": "u", "port": 2222, "identity_file": "/k",
	})
	if err != nil {
		t.Fatal(err)
	}
	s, ok := r.(SSH)
	if !ok {
		t.Fatalf("want SSH, got %T", r)
	}
	if s.Host != "h" || s.User != "u" || s.Port != 2222 || s.IdentityFile != "/k" {
		t.Fatalf("bad SSH: %+v", s)
	}
}

func TestSSHFromConfig_RequiresHostUser(t *testing.T) {
	if _, err := SSHFromConfig(map[string]any{"host": "h"}); err == nil {
		t.Fatal("missing user must error")
	}
	if _, err := SSHFromConfig(map[string]any{"user": "u"}); err == nil {
		t.Fatal("missing host must error")
	}
}

func TestSSHFromConfig_PortFromFloatAndExpandTilde(t *testing.T) {
	r, _ := SSHFromConfig(map[string]any{"host": "h", "user": "u", "port": float64(22), "identity_file": "~/x"})
	s := r.(SSH)
	if s.Port != 22 {
		t.Fatalf("port float: %d", s.Port)
	}
	if s.IdentityFile == "~/x" {
		t.Fatal("~ should be expanded")
	}
}
