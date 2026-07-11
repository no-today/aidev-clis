package dataease

import (
	"testing"
	"time"
)

func TestParseConfig_RequiresBaseURL(t *testing.T) {
	_, err := ParseConfig("local", map[string]any{"data_source_id": "ds-1"})
	requireCode(t, err, "DATAEASE_BASE_URL_MISSING")
}

func TestParseConfig_RequiresDataSourceID(t *testing.T) {
	_, err := ParseConfig("local", map[string]any{"base_url": "https://example.test/dataease"})
	requireCode(t, err, "DATAEASE_DATA_SOURCE_ID_MISSING")
}

func TestParseConfig_DefaultsAndTrims(t *testing.T) {
	cfg, err := ParseConfig("local_pay", map[string]any{
		"base_url":         "https://example.test/dataease/",
		"data_source_id":   "8d176702-2684-4371-93c4-bee7bc1e13f2",
		"login_credential": "dataease.login",
		"timeout_seconds":  float64(45),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EnvName != "local_pay" {
		t.Errorf("EnvName = %q", cfg.EnvName)
	}
	if cfg.BaseURL != "https://example.test/dataease" {
		t.Errorf("BaseURL = %q (trailing slash not trimmed)", cfg.BaseURL)
	}
	if cfg.DataSourceID != "8d176702-2684-4371-93c4-bee7bc1e13f2" {
		t.Errorf("DataSourceID = %q", cfg.DataSourceID)
	}
	if cfg.SessionKey != "dataease.local_pay.session" {
		t.Errorf("SessionKey = %q (env-scoped default)", cfg.SessionKey)
	}
	if cfg.LoginCredential != "dataease.login" {
		t.Errorf("LoginCredential = %q", cfg.LoginCredential)
	}
	if cfg.Timeout != 45*time.Second {
		t.Errorf("Timeout = %v", cfg.Timeout)
	}
}

func TestParseConfig_RejectsNonPositiveTimeout(t *testing.T) {
	_, err := ParseConfig("local", map[string]any{
		"base_url":        "https://example.test/dataease",
		"data_source_id":  "ds-1",
		"timeout_seconds": float64(0),
	})
	requireCode(t, err, "DATAEASE_TIMEOUT_INVALID")
}

func TestParseConfig_UsesConfiguredSharedSessionKey(t *testing.T) {
	cfg, err := ParseConfig("local_other", map[string]any{
		"base_url":       "https://example.test/dataease",
		"data_source_id": "ds-1",
		"session":        "dataease.jnuh.admin.session",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SessionKey != "dataease.jnuh.admin.session" {
		t.Errorf("SessionKey = %q", cfg.SessionKey)
	}
}

func TestParseConfig_RejectsBadSessionName(t *testing.T) {
	_, err := ParseConfig("local", map[string]any{
		"base_url":       "https://example.test/dataease",
		"data_source_id": "ds-1",
		"session":        "../escape",
	})
	requireCode(t, err, "CRED_BAD_NAME")
}

func TestParseLoginCredential_RequiresUsernameAndPassword(t *testing.T) {
	_, err := ParseLoginCredential([]byte(`{"username":"u"}`))
	requireCode(t, err, "DATAEASE_LOGIN_PASSWORD_MISSING")

	_, err = ParseLoginCredential([]byte(`{"password":"p"}`))
	requireCode(t, err, "DATAEASE_LOGIN_USERNAME_MISSING")
}

func TestParseLoginCredential_DefaultsLoginType(t *testing.T) {
	cred, err := ParseLoginCredential([]byte(`{"username":"encrypted-user","password":"encrypted-pass"}`))
	if err != nil {
		t.Fatal(err)
	}
	if cred.Username != "encrypted-user" || cred.Password != "encrypted-pass" || cred.LoginType != 0 {
		t.Errorf("cred = %+v", cred)
	}
}
