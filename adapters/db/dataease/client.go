package dataease

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/dbcli"
)

// Client talks to a DataEase instance over net/http. It never follows redirects:
// an expired session bounces to the login page with a 3xx, which we treat as an
// auth signal rather than chasing into a private/internal location.
type Client struct {
	baseURL      string
	dataSourceID string
	httpClient   *http.Client
}

func NewClient(cfg *Config) *Client {
	timeout := defaultTimeout
	baseURL := ""
	dataSourceID := ""
	if cfg != nil {
		if cfg.Timeout > 0 {
			timeout = cfg.Timeout
		}
		baseURL = strings.TrimRight(cfg.BaseURL, "/")
		dataSourceID = cfg.DataSourceID
	}
	return &Client{
		baseURL:      baseURL,
		dataSourceID: dataSourceID,
		httpClient: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// Login posts the (pre-encrypted) credential to /api/auth/login and captures the
// JWT from the response body, falling back to the Authorization header.
func (c *Client) Login(ctx context.Context, cred LoginCredential) (*Session, error) {
	body, err := json.Marshal(map[string]any{
		"username":  cred.Username,
		"password":  cred.Password,
		"loginType": cred.LoginType,
	})
	if err != nil {
		return nil, errs.General("DATAEASE_LOGIN_REQUEST_MARSHAL_FAILED", err.Error())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/auth/login", bytes.NewReader(body))
	if err != nil {
		return nil, errs.Config("DATAEASE_LOGIN_REQUEST_INVALID", err.Error())
	}
	setRequestHeaders(req, c.baseURL, "")
	result, err := c.do(req)
	if err != nil {
		return nil, errs.Remote("DATAEASE_HTTP_ERROR", fmt.Sprintf("login request failed: %v", err))
	}
	if result.StatusCode < 200 || result.StatusCode >= 300 {
		return nil, errs.Remote("DATAEASE_HTTP_ERROR", fmt.Sprintf("login returned HTTP %d: %s", result.StatusCode, string(result.Body)))
	}
	token, err := parseLoginToken(result.Body)
	if err != nil {
		return nil, err
	}
	if token == "" {
		token = result.Header.Get("Authorization")
	}
	if token == "" {
		return nil, errs.Auth("DATAEASE_LOGIN_NO_TOKEN", "DataEase login response did not include a token")
	}
	return &Session{Token: token, BaseURL: c.baseURL}, nil
}

// Query base64-encodes the SQL into a sqlPreview request and projects the response.
func (c *Client) Query(ctx context.Context, sess *Session, sqlStr string) (*dbcli.Result, error) {
	if sess == nil || sess.Token == "" {
		return nil, errs.Auth("DATAEASE_SESSION_NO_TOKEN", "dataease session has no token")
	}
	info, err := json.Marshal(map[string]any{
		"sql":                base64.StdEncoding.EncodeToString([]byte(sqlStr)),
		"isBase64Encryption": true,
	})
	if err != nil {
		return nil, errs.General("DATAEASE_QUERY_INFO_MARSHAL_FAILED", err.Error())
	}
	payload, err := json.Marshal(map[string]any{
		"dataSourceId":       c.dataSourceID,
		"type":               "sql",
		"mode":               0,
		"sqlVariableDetails": "[]",
		"info":               string(info),
	})
	if err != nil {
		return nil, errs.General("DATAEASE_QUERY_REQUEST_MARSHAL_FAILED", err.Error())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/dataset/table/sqlPreview", bytes.NewReader(payload))
	if err != nil {
		return nil, errs.Config("DATAEASE_QUERY_REQUEST_INVALID", err.Error())
	}
	setRequestHeaders(req, c.baseURL, sess.Token)
	result, err := c.do(req)
	if err != nil {
		return nil, errs.Remote("DATAEASE_HTTP_ERROR", fmt.Sprintf("query request failed: %v", err))
	}
	if isWAFBlock(result) {
		return nil, errs.Remote("DATAEASE_WAF_BLOCKED", "DataEase WAF blocked sqlPreview request")
	}
	if isAuthRedirect(result) {
		return nil, errs.Auth("DATAEASE_AUTH_EXPIRED", authRedirectMessage(result))
	}
	if result.StatusCode == http.StatusUnauthorized || result.StatusCode == http.StatusForbidden {
		msg := strings.TrimSpace(string(result.Body))
		if msg == "" {
			msg = fmt.Sprintf("query returned HTTP %d", result.StatusCode)
		}
		return nil, errs.Auth("DATAEASE_AUTH_EXPIRED", msg)
	}
	if result.StatusCode < 200 || result.StatusCode >= 300 {
		return nil, errs.Remote("DATAEASE_HTTP_ERROR", fmt.Sprintf("query returned HTTP %d: %s", result.StatusCode, string(result.Body)))
	}
	return ParseQueryResponse(result.Body)
}

type httpResult struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

func (c *Client) do(req *http.Request) (*httpResult, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return &httpResult{StatusCode: resp.StatusCode, Header: resp.Header, Body: respBody}, nil
}

func isAuthRedirect(result *httpResult) bool {
	// A logged-in sqlPreview call returns 200 with a JSON envelope; an expired
	// session bounces to the login page with a 3xx redirect. We never follow
	// these (CheckRedirect returns ErrUseLastResponse), so any 3xx on this API
	// endpoint means the session is no longer authenticated. Some deployments tag
	// the bounce with Authentication-Status/onAccessDeniedMsg and some return a
	// bare 302; the status range alone is the reliable signal.
	return result != nil && result.StatusCode >= 300 && result.StatusCode < 400
}

func authRedirectMessage(result *httpResult) string {
	if result == nil {
		return "DataEase authentication redirect"
	}
	if location := strings.TrimSpace(result.Header.Get("Location")); location != "" {
		return fmt.Sprintf("DataEase authentication redirect to %s", location)
	}
	return fmt.Sprintf("DataEase authentication redirect returned HTTP %d", result.StatusCode)
}

// isWAFBlock detects the DataEase backend WAF (gyfe) bounce: a 403 carrying an
// HTML body instead of the JSON envelope. We surface it as a clear error rather
// than treating it as auth expiry (refreshing the session would not help).
func isWAFBlock(result *httpResult) bool {
	if result == nil || result.StatusCode != http.StatusForbidden {
		return false
	}
	contentType := strings.ToLower(result.Header.Get("Content-Type"))
	return strings.Contains(contentType, "text/html") ||
		bytes.Contains(result.Body, []byte("<html")) ||
		bytes.Contains(result.Body, []byte("您的请求可能存在威胁"))
}

const dataEaseBrowserUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36"

// setRequestHeaders applies the browser-mimicking headers DataEase's WAF expects,
// then the token (in both the Authorization header and the Cookie).
func setRequestHeaders(req *http.Request, baseURL, token string) {
	setCommonHeaders(req, baseURL)
	if token == "" {
		req.Header.Set("Cookie", "request-time-out=100; language=zh_CN")
		return
	}
	req.Header.Set("Authorization", token)
	req.Header.Set("Cookie", "request-time-out=100; language=zh_CN; Authorization="+token)
}

func setCommonHeaders(req *http.Request, baseURL string) {
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("LINK-PWD-TOKEN", "null")
	req.Header.Set("User-Agent", dataEaseBrowserUserAgent)
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("sec-ch-ua", `"Chromium";v="148", "Google Chrome";v="148", "Not/A)Brand";v="99"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"macOS"`)
	if origin := originFromBaseURL(baseURL); origin != "" {
		req.Header.Set("Origin", origin)
	}
	if referer := refererFromBaseURL(baseURL); referer != "" {
		req.Header.Set("Referer", referer)
	}
}

func originFromBaseURL(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func refererFromBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return ""
	}
	return baseURL + "/login"
}

func parseLoginToken(body []byte) (string, error) {
	var envelope struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", errs.Remote("DATAEASE_BAD_RESPONSE", fmt.Sprintf("invalid DataEase login response JSON: %v", err))
	}
	if !envelope.Success {
		msg := envelope.Message
		if msg == "" {
			msg = "DataEase login failed"
		}
		return "", errs.Auth("DATAEASE_LOGIN_FAILED", msg)
	}
	return envelope.Data.Token, nil
}
