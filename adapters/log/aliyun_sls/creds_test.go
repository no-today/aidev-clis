package aliyunsls

import (
	"strings"
	"testing"
)

func TestParseCredential_AllFields(t *testing.T) {
	data := []byte(`{"access_key_id":"AKID123","access_key_secret":"SECRET456","security_token":"TOKEN789"}`)
	c, err := ParseCredential(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.AccessKeyID != "AKID123" {
		t.Fatalf("AccessKeyID = %q, want AKID123", c.AccessKeyID)
	}
	if c.AccessKeySecret != "SECRET456" {
		t.Fatalf("AccessKeySecret = %q, want SECRET456", c.AccessKeySecret)
	}
	if c.SecurityToken != "TOKEN789" {
		t.Fatalf("SecurityToken = %q, want TOKEN789", c.SecurityToken)
	}
}

func TestParseCredential_NoSecurityToken(t *testing.T) {
	data := []byte(`{"access_key_id":"AKID123","access_key_secret":"SECRET456"}`)
	c, err := ParseCredential(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.AccessKeyID != "AKID123" {
		t.Fatalf("AccessKeyID = %q, want AKID123", c.AccessKeyID)
	}
	if c.AccessKeySecret != "SECRET456" {
		t.Fatalf("AccessKeySecret = %q, want SECRET456", c.AccessKeySecret)
	}
	if c.SecurityToken != "" {
		t.Fatalf("SecurityToken = %q, want empty", c.SecurityToken)
	}
}

func TestParseCredential_InvalidJSON(t *testing.T) {
	_, err := ParseCredential([]byte("not json"))
	if err == nil {
		t.Fatal("want error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "SLS_CRED_INVALID_JSON") {
		t.Fatalf("error = %q, want SLS_CRED_INVALID_JSON", err.Error())
	}
}

func TestParseCredential_MissingAccessKeyID(t *testing.T) {
	data := []byte(`{"access_key_secret":"SECRET456"}`)
	_, err := ParseCredential(data)
	if err == nil {
		t.Fatal("want error for missing access_key_id")
	}
	if !strings.Contains(err.Error(), "SLS_AK_MISSING") {
		t.Fatalf("error = %q, want SLS_AK_MISSING", err.Error())
	}
}

func TestParseCredential_MissingAccessKeySecret(t *testing.T) {
	data := []byte(`{"access_key_id":"AKID123"}`)
	_, err := ParseCredential(data)
	if err == nil {
		t.Fatal("want error for missing access_key_secret")
	}
	if !strings.Contains(err.Error(), "SLS_SK_MISSING") {
		t.Fatalf("error = %q, want SLS_SK_MISSING", err.Error())
	}
}
