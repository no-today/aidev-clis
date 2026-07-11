// Package aliyunsls implements the built-in Aliyun SLS log adapter for logcli.
// It reads logs through the official SLS GetLogs OpenAPI using a static
// AccessKey (AK/SK) and signature v1 (hmac-sha1).
package aliyunsls

import (
	"strings"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// DefaultCredential is the credstore filename used when an env omits `credential`.
const DefaultCredential = "sls.ak"

// SLSConfig is the typed view of the per-env adapter block.
type SLSConfig struct {
	Project    string
	Logstore   string
	Endpoint   string // region host, e.g. cn-hangzhou.log.aliyuncs.com
	Credential string // logical name → credstore file (default "sls.ak")
	TraceField string // default "traceId"
	NoProxy    bool   // bypass HTTP(S)_PROXY env for this target (corp proxies often can't reach SLS)
}

// ParseConfig converts the raw env block into SLSConfig.
func ParseConfig(raw map[string]interface{}) (*SLSConfig, error) {
	cfg := &SLSConfig{TraceField: "traceId", Credential: DefaultCredential}

	project, _ := raw["project"].(string)
	if project == "" {
		return nil, errs.Config("SLS_PROJECT_MISSING", "sls env block missing required field 'project'")
	}
	cfg.Project = project

	logstore, _ := raw["logstore"].(string)
	if logstore == "" {
		return nil, errs.Config("SLS_LOGSTORE_MISSING", "sls env block missing required field 'logstore'")
	}
	cfg.Logstore = logstore

	endpoint, _ := raw["endpoint"].(string)
	if endpoint == "" {
		return nil, errs.Config("SLS_ENDPOINT_MISSING",
			"sls env block missing required field 'endpoint' (region host, e.g. cn-hangzhou.log.aliyuncs.com)")
	}
	endpoint = strings.TrimPrefix(endpoint, "https://")
	endpoint = strings.TrimPrefix(endpoint, "http://")
	if strings.ContainsAny(endpoint, "/?") {
		return nil, errs.Config("SLS_ENDPOINT_INVALID",
			"sls 'endpoint' must be a bare region host (e.g. cn-hangzhou.log.aliyuncs.com), not a URL with a path")
	}
	cfg.Endpoint = endpoint

	if v, ok := raw["credential"].(string); ok && v != "" {
		cfg.Credential = v
	}
	if v, ok := raw["trace_field"].(string); ok && v != "" {
		cfg.TraceField = v
	}
	cfg.NoProxy, _ = raw["no_proxy"].(bool)
	return cfg, nil
}
