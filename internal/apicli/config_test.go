package apicli

import (
	"os"
	"path/filepath"
	"testing"
)

func writeHome(t *testing.T, apicliYAML, actorsYAML string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	if apicliYAML != "" {
		if err := os.WriteFile(filepath.Join(home, "apicli.yaml"), []byte(apicliYAML), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if actorsYAML != "" {
		if err := os.WriteFile(filepath.Join(home, "actors.yaml"), []byte(actorsYAML), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

const sampleAPICLI = `
apps:
  svc-login:
    base_url: http://uat.example.com
    envs:
      pre: http://pre.example.com
    default_actor: alice
    auth:
      kind: flow
      vars_required: [phoneNo, password]
      flow:
        - request: |
            POST /auth/login
            Content-Type: application/json

            {"phoneNo":"{{phoneNo}}","password":"{{password}}"}
          capture:
            token: body.data.token
      inject:
        header: "Authorization: Bearer {{token}}"
    response:
      ok_when: "body.code == 0"
      expired_when: "body.code == 401"
  svc-open:
    base_url: https://svc-open.example.com
    auth:
      kind: none
    response:
      ok_when: "status == 200"
`

func TestLoadConfigApp(t *testing.T) {
	writeHome(t, sampleAPICLI, "")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	doc, ok := cfg.Apps["svc-login"]
	if !ok {
		t.Fatal("svc-login app missing")
	}
	if doc.BaseURL != "http://uat.example.com" {
		t.Errorf("base_url = %q", doc.BaseURL)
	}
	if doc.Envs["pre"] != "http://pre.example.com" {
		t.Errorf("env pre = %q", doc.Envs["pre"])
	}
	if doc.DefaultActor != "alice" {
		t.Errorf("default_actor = %q", doc.DefaultActor)
	}
	if doc.Auth.Kind != "flow" || len(doc.Auth.VarsRequired) != 2 {
		t.Errorf("auth = %+v", doc.Auth)
	}
	if cfg.Apps["svc-open"].Auth.Kind != "none" {
		t.Errorf("svc-open auth kind = %q", cfg.Apps["svc-open"].Auth.Kind)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	writeHome(t, "", "")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected error for missing apicli.yaml")
	}
}

func TestLoadConfigScene(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "apicli.yaml"), []byte(`
apps:
  svc-login: { base_url: "http://x", scene: companyA, auth: { kind: none } }
  svc-open:  { base_url: "http://y", auth: { kind: none } }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AIDEV_CLIS_HOME", dir)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Apps["svc-login"].Scene != "companyA" {
		t.Errorf("svc-login scene = %q, want companyA", cfg.Apps["svc-login"].Scene)
	}
	if cfg.Apps["svc-open"].Scene != "" {
		t.Errorf("svc-open scene = %q, want empty (global)", cfg.Apps["svc-open"].Scene)
	}
}
