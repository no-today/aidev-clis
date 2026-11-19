package jcli

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/no-today/aidev-clis/internal/core/cred"
	"github.com/no-today/aidev-clis/internal/core/errs"
)

// Client is an authenticated Jenkins REST client. The credential never leaves it.
type Client struct {
	base  string
	user  string
	token string
	http  *http.Client

	pollEvery time.Duration // inter-poll delay (tests set this small)

	crumbField string
	crumbVal   string
	crumbDone  bool
}

// NewClient reads the {user,api_token} credential and builds a TLS-configured
// http.Client. base_url trailing slash is trimmed.
func NewClient(c *Config) (*Client, error) {
	raw, err := cred.Read(c.Credential)
	if err != nil {
		return nil, err
	}
	var creds struct {
		User     string `json:"user"`
		APIToken string `json:"api_token"`
	}
	if err := json.Unmarshal(raw, &creds); err != nil || creds.User == "" || creds.APIToken == "" {
		return nil, errs.Auth("CRED_INVALID", "credential must be JSON {\"user\":...,\"api_token\":...}")
	}
	tlsCfg := &tls.Config{InsecureSkipVerify: c.InsecureSkipVerify} //nolint:gosec // opt-in for internal Jenkins
	if c.CACert != "" {
		pem, err := os.ReadFile(c.CACert)
		if err != nil {
			return nil, errs.Config("CA_CERT_UNREADABLE", err.Error())
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errs.Config("CA_CERT_INVALID", "ca_cert is not valid PEM")
		}
		tlsCfg.RootCAs = pool
	}
	return &Client{
		base:      strings.TrimRight(c.BaseURL, "/"),
		user:      creds.User,
		token:     creds.APIToken,
		pollEvery: 2 * time.Second,
		http:      &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: tlsCfg}},
	}, nil
}

// do issues an authenticated request to path (a /-rooted path or absolute URL),
// adding the CSRF crumb on mutating methods. form may be nil or url-encoded.
func (c *Client) do(ctx context.Context, method, path string, form url.Values) (*http.Response, error) {
	u := path
	if strings.HasPrefix(path, "/") {
		u = c.base + path
	}
	var bodyReader io.Reader
	if form != nil {
		bodyReader = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
	if err != nil {
		return nil, errs.Config("BAD_REQUEST", err.Error())
	}
	req.SetBasicAuth(c.user, c.token)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if method != http.MethodGet && method != http.MethodHead {
		if field, val := c.crumb(ctx); field != "" {
			req.Header.Set(field, val)
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errs.Remote("CONNECT_FAILED", err.Error())
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		resp.Body.Close()
		return nil, errs.Auth("AUTH_FAILED", "Jenkins rejected the credential (HTTP "+resp.Status+")")
	}
	return resp, nil
}

// crumb fetches the CSRF crumb once, best-effort (absence is not an error).
func (c *Client) crumb(ctx context.Context) (string, string) {
	if c.crumbDone {
		return c.crumbField, c.crumbVal
	}
	c.crumbDone = true
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/crumbIssuer/api/json", nil)
	req.SetBasicAuth(c.user, c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", ""
	}
	var cr struct {
		Field string `json:"crumbRequestField"`
		Crumb string `json:"crumb"`
	}
	if json.NewDecoder(resp.Body).Decode(&cr) == nil {
		c.crumbField, c.crumbVal = cr.Field, cr.Crumb
	}
	return c.crumbField, c.crumbVal
}

// jobPath maps a (possibly /-separated) job name to its /job/a/job/b URL path.
func jobPath(job string) string {
	segs := strings.Split(job, "/")
	var b strings.Builder
	for _, s := range segs {
		if s == "" {
			continue
		}
		b.WriteString("/job/")
		b.WriteString(url.PathEscape(s))
	}
	return b.String()
}

// itoa is a small shared int→string helper used across the package.
func itoa(n int) string { return strconv.Itoa(n) }
