package jcli

import "testing"

func TestParseConfig_Deploy(t *testing.T) {
	raw := map[string]any{
		"base_url":   "https://j",
		"credential": "c",
		"groups": map[string]any{
			"main": map[string]any{
				"deploy": map[string]any{
					"params":  map[string]any{"branch": "${branch}"},
					"extract": map[string]any{"tag": `reg:(\d+)`},
					"steps": []any{
						[]any{"kubectl", "set", "image", "deploy/${service}", 3},
						[]any{"bash", "-c", "echo hi"},
					},
					"step_binaries": []any{"kubectl", "bash"},
					"step_env":      map[string]any{"KUBECONFIG": "${vars.kc}"},
				},
				"vars": map[string]any{"kc": "/etc/kube"},
			},
		},
	}
	c, err := ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	g, err := c.Group("main")
	if err != nil {
		t.Fatal(err)
	}
	if g.Deploy == nil || g.Deploy.Params["branch"] != "${branch}" {
		t.Fatalf("deploy params: %+v", g.Deploy)
	}
	if len(g.Deploy.Steps) != 2 || g.Deploy.Steps[0][4] != "3" {
		t.Fatalf("steps: %+v", g.Deploy.Steps)
	}
	if g.Deploy.StepEnv["KUBECONFIG"] != "${vars.kc}" || g.Vars["kc"] != "/etc/kube" {
		t.Fatalf("env/vars: %+v %+v", g.Deploy.StepEnv, g.Vars)
	}
}
