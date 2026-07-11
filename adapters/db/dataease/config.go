package dataease

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

const defaultTimeout = 30 * time.Second

// Config is the resolved dataease env block.
type Config struct {
	EnvName         string
	BaseURL         string
	DataSourceID    string
	SessionKey      string
	LoginCredential string
	Timeout         time.Duration
}

// LoginCredential is the JSON shape of ~/.aidev-clis/credentials/<login_credential>.
type LoginCredential struct {
	Username  string `json:"username"`
	Password  string `json:"password"`
	LoginType int    `json:"loginType"`
}

// ParseConfig validates the env block. base_url and data_source_id are required;
// session defaults to dataease.<env>.session. use_curl is intentionally not
// supported (dataease is net/http only, cross-platform).
func ParseConfig(envName string, raw map[string]any) (*Config, error) {
	baseURL, _ := raw["base_url"].(string)
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, errs.Config("DATAEASE_BASE_URL_MISSING", "dataease env requires base_url")
	}
	dataSourceID, _ := raw["data_source_id"].(string)
	dataSourceID = strings.TrimSpace(dataSourceID)
	if dataSourceID == "" {
		return nil, errs.Config("DATAEASE_DATA_SOURCE_ID_MISSING", "dataease env requires data_source_id")
	}
	sessionKey, _ := raw["session"].(string)
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		sessionKey = fmt.Sprintf("dataease.%s.session", envName)
	}
	if err := validateName(sessionKey); err != nil {
		return nil, err
	}
	loginCredential, _ := raw["login_credential"].(string)
	loginCredential = strings.TrimSpace(loginCredential)
	if loginCredential != "" {
		if err := validateName(loginCredential); err != nil {
			return nil, err
		}
	}
	timeout := defaultTimeout
	if v, ok := raw["timeout_seconds"]; ok {
		seconds, err := numericSeconds(v)
		if err != nil {
			return nil, err
		}
		timeout = time.Duration(seconds) * time.Second
	}
	return &Config{
		EnvName:         envName,
		BaseURL:         baseURL,
		DataSourceID:    dataSourceID,
		SessionKey:      sessionKey,
		LoginCredential: loginCredential,
		Timeout:         timeout,
	}, nil
}

func numericSeconds(v any) (int, error) {
	switch n := v.(type) {
	case int:
		if n <= 0 {
			return 0, errs.Config("DATAEASE_TIMEOUT_INVALID", "timeout_seconds must be positive")
		}
		return n, nil
	case int64:
		if n <= 0 {
			return 0, errs.Config("DATAEASE_TIMEOUT_INVALID", "timeout_seconds must be positive")
		}
		return int(n), nil
	case float64:
		if n <= 0 || n != float64(int(n)) {
			return 0, errs.Config("DATAEASE_TIMEOUT_INVALID", "timeout_seconds must be a positive integer")
		}
		return int(n), nil
	default:
		return 0, errs.Config("DATAEASE_TIMEOUT_INVALID", "timeout_seconds must be a positive integer")
	}
}

// ParseLoginCredential parses the login credential JSON; username/password required.
func ParseLoginCredential(data []byte) (*LoginCredential, error) {
	var cred LoginCredential
	if err := json.Unmarshal(data, &cred); err != nil {
		return nil, errs.Config("DATAEASE_LOGIN_CREDENTIAL_INVALID_JSON", fmt.Sprintf("invalid dataease login credential JSON: %v", err))
	}
	if strings.TrimSpace(cred.Username) == "" {
		return nil, errs.Config("DATAEASE_LOGIN_USERNAME_MISSING", "dataease login credential requires username")
	}
	if strings.TrimSpace(cred.Password) == "" {
		return nil, errs.Config("DATAEASE_LOGIN_PASSWORD_MISSING", "dataease login credential requires password")
	}
	return &cred, nil
}
