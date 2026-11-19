package aliyunsls

import (
	"encoding/json"
	"fmt"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// Credential is the AK/SK JSON blob stored in the credstore file named by the
// env's `credential` field.
type Credential struct {
	AccessKeyID     string `json:"access_key_id"`
	AccessKeySecret string `json:"access_key_secret"`
	SecurityToken   string `json:"security_token,omitempty"` // optional STS token
}

// ParseCredential decodes the stored credential bytes.
func ParseCredential(data []byte) (*Credential, error) {
	c := &Credential{}
	if err := json.Unmarshal(data, c); err != nil {
		return nil, errs.Config("SLS_CRED_INVALID_JSON",
			fmt.Sprintf("sls credential is not valid JSON: %v", err))
	}
	if c.AccessKeyID == "" {
		return nil, errs.Auth("SLS_AK_MISSING",
			"sls credential missing 'access_key_id'")
	}
	if c.AccessKeySecret == "" {
		return nil, errs.Auth("SLS_SK_MISSING",
			"sls credential missing 'access_key_secret'")
	}
	return c, nil
}
