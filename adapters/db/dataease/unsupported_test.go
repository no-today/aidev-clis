package dataease

import "testing"

func TestRun_RejectsCatalogVerbs(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	raw := map[string]any{
		"base_url":       "https://example.test/dataease",
		"data_source_id": "ds-1",
	}
	for _, verb := range []string{"databases", "tables", "describe", "DATABASES", "Describe"} {
		_, err := runDriver(t, raw, verb)
		requireCode(t, err, "DATAEASE_UNSUPPORTED_VERB")
	}
}
