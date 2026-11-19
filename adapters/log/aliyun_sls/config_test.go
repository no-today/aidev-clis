package aliyunsls

import (
	"strings"
	"testing"
)

func TestParseConfig_Defaults(t *testing.T) {
	cfg, err := ParseConfig(map[string]interface{}{
		"project":  "p1",
		"logstore": "ls1",
		"endpoint": "cn-hangzhou.log.aliyuncs.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Project != "p1" {
		t.Fatalf("Project = %q, want p1", cfg.Project)
	}
	if cfg.Logstore != "ls1" {
		t.Fatalf("Logstore = %q, want ls1", cfg.Logstore)
	}
	if cfg.Endpoint != "cn-hangzhou.log.aliyuncs.com" {
		t.Fatalf("Endpoint = %q, want cn-hangzhou.log.aliyuncs.com", cfg.Endpoint)
	}
	if cfg.Credential != DefaultCredential {
		t.Fatalf("Credential = %q, want %q (credential default)", cfg.Credential, DefaultCredential)
	}
	if cfg.TraceField != "traceId" {
		t.Fatalf("TraceField = %q, want traceId (trace_field default)", cfg.TraceField)
	}
}

func TestParseConfig_RequiredFields(t *testing.T) {
	cases := []struct {
		name string
		raw  map[string]interface{}
		code string
	}{
		{"no project", map[string]interface{}{"logstore": "l", "endpoint": "e.log.aliyuncs.com"}, "SLS_PROJECT_MISSING"},
		{"no logstore", map[string]interface{}{"project": "p", "endpoint": "e.log.aliyuncs.com"}, "SLS_LOGSTORE_MISSING"},
		{"no endpoint", map[string]interface{}{"project": "p", "logstore": "l"}, "SLS_ENDPOINT_MISSING"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseConfig(tc.raw)
			if err == nil {
				t.Fatalf("want error for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.code) {
				t.Fatalf("error = %q, want to contain %q", err.Error(), tc.code)
			}
		})
	}
}

func TestParseConfig_RejectsConsoleURLEndpoint(t *testing.T) {
	_, err := ParseConfig(map[string]interface{}{
		"project":  "p",
		"logstore": "l",
		"endpoint": "https://sls.console.aliyun.com/console/x.json",
	})
	if err == nil {
		t.Fatal("want error for console URL endpoint")
	}
	if !strings.Contains(err.Error(), "SLS_ENDPOINT_INVALID") {
		t.Fatalf("error = %q, want SLS_ENDPOINT_INVALID", err.Error())
	}
}

func TestParseConfig_StripsSchemePrefix(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string
	}{
		{"https prefix", "https://cn-hangzhou.log.aliyuncs.com"},
		{"http prefix", "http://cn-hangzhou.log.aliyuncs.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := ParseConfig(map[string]interface{}{
				"project":  "p",
				"logstore": "l",
				"endpoint": tc.endpoint,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.Endpoint != "cn-hangzhou.log.aliyuncs.com" {
				t.Fatalf("Endpoint = %q, want cn-hangzhou.log.aliyuncs.com", cfg.Endpoint)
			}
		})
	}
}

func TestParseConfig_AllFields(t *testing.T) {
	cfg, err := ParseConfig(map[string]interface{}{
		"project":     "myproject",
		"logstore":    "mylogstore",
		"endpoint":    "cn-beijing.log.aliyuncs.com",
		"credential":  "custom.ak",
		"trace_field": "request_id",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Project != "myproject" {
		t.Fatalf("Project = %q, want myproject", cfg.Project)
	}
	if cfg.Logstore != "mylogstore" {
		t.Fatalf("Logstore = %q, want mylogstore", cfg.Logstore)
	}
	if cfg.Endpoint != "cn-beijing.log.aliyuncs.com" {
		t.Fatalf("Endpoint = %q, want cn-beijing.log.aliyuncs.com", cfg.Endpoint)
	}
	if cfg.Credential != "custom.ak" {
		t.Fatalf("Credential = %q, want custom.ak", cfg.Credential)
	}
	if cfg.TraceField != "request_id" {
		t.Fatalf("TraceField = %q, want request_id", cfg.TraceField)
	}
}
