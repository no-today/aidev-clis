package apicli

import (
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"
)

// CallResult is the classified outcome of Call.
type CallResult struct {
	Status         int               `json:"status_code"`
	OK             bool              `json:"ok"`
	Headers        map[string]string `json:"headers"`
	Body           any               `json:"body,omitempty"`
	TraceID        string            `json:"trace_id,omitempty"`
	BodyFile       string            `json:"body_file,omitempty"`
	BodyBytes      int64             `json:"body_bytes,omitempty"`
	ContentLength  int64             `json:"content_length,omitempty"`
	StreamComplete bool              `json:"stream_complete,omitempty"`
	SHA256         string            `json:"sha256,omitempty"`
	HeadersFile    string            `json:"headers_file,omitempty"`
	Relogged       bool              `json:"-"`
}

// Call loads the session, sends the request, auto-relogins once on expiry, and
// classifies via ok_when. Returns SESSION_EXPIRED (errs.Auth) when it cannot.
// Apps with auth.kind == "none" skip session handling entirely.
func Call(tg *Target, req *CallRequest) (*CallResult, error) {
	noAuth := tg.Auth.Kind == "none"

	var sess Session
	freshLogin := false
	if !noAuth {
		have := false
		if sess, have = LoadSession(tg); !have {
			fresh, err := Login(tg) // may return SESSION_EXPIRED for interactive
			if err != nil {
				return nil, err
			}
			_ = SaveSession(tg, fresh)
			sess = fresh
			freshLogin = true
		}
	}

	resp, err := DoRequest(tg, req, sess)
	if err != nil {
		return nil, err
	}

	// Auto-relogin only when reusing a STORED session that turned out stale.
	// A just-completed fresh login (freshLogin) is never re-logged-in here —
	// that guarantees at most one login per Call (spec §4: "re-login once").
	relogged := false
	if !noAuth && !freshLogin {
		expired, perr := EvalPredicate(tg.Response.ExpiredWhen, resp)
		if perr != nil {
			return nil, perr
		}
		if expired {
			fresh, lerr := Login(tg)
			if lerr != nil {
				return nil, lerr
			}
			_ = SaveSession(tg, fresh)
			relogged = true
			// Retry result is returned unconditionally — expired_when is NOT
			// re-checked, so there is no relogin loop. If the retry is also
			// stale, ok_when yields OK=false and the caller sees the failure.
			resp, err = DoRequest(tg, req, fresh)
			if err != nil {
				return nil, err
			}
		}
	}

	ok, perr := EvalPredicate(tg.Response.OKWhen, resp)
	if perr != nil {
		return nil, perr
	}
	res := &CallResult{
		Status: resp.Status, OK: ok, Headers: resp.Headers, Relogged: relogged,
	}
	if resp.BodyFile != "" {
		res.BodyFile = resp.BodyFile
		res.BodyBytes = resp.BodyBytes
		res.ContentLength = resp.ContentLength
		res.StreamComplete = resp.StreamComplete
		res.SHA256 = resp.SHA256
		res.HeadersFile = req.HeadersFile
	} else {
		res.Body = decodeBody(resp.Body)
	}
	res.TraceID = extractTrace(tg.TraceField, resp)
	return res, nil
}

// extractTrace pulls a trace id from a response per a tiny dotpath spec:
// header.<name> | body.<dotpath> | bare <x> (== body.<x>). "" on miss.
func extractTrace(field string, r *RawResponse) string {
	if field == "" {
		return ""
	}
	if name, ok := strings.CutPrefix(field, "header."); ok {
		v, _ := r.header(name)
		return v
	}
	path := strings.TrimPrefix(field, "body.")
	return gjson.GetBytes(r.Body, path).String()
}

// decodeBody returns parsed JSON when possible, else the raw string.
func decodeBody(b []byte) any {
	var v any
	if json.Unmarshal(b, &v) == nil {
		return v
	}
	return string(b)
}
