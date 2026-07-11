package apicli

import (
	"fmt"
	"strings"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// canAutoLogin reports whether all vars_required are present (non-interactive).
func (tg *Target) canAutoLogin() bool {
	if tg.Auth.Kind != "flow" {
		return false
	}
	have := mergeVars(tg.Auth.VarsDefaults, tg.Vars)
	for _, v := range tg.Auth.VarsRequired {
		if have[v] == "" {
			return false
		}
	}
	return len(tg.Auth.Flow) > 0
}

// Login runs the app's auth flow and returns a fresh Session (without saving).
func Login(tg *Target) (Session, error) {
	if !tg.canAutoLogin() {
		return Session{}, errs.Auth("SESSION_EXPIRED",
			"login requires interactive/missing vars; run `apicli login <app>`")
	}
	base := mergeVars(tg.Auth.VarsDefaults, tg.Vars) // defaults lowest, actor/CLI win
	captured := map[string]string{}
	sessionCookie := ""
	for i, step := range tg.Auth.Flow {
		req := parseRawHTTP(step.Request, mergeVars(base, captured))
		resp, err := DoRequest(tg, req, Session{Vars: captured, Cookie: sessionCookie})
		if err != nil {
			return Session{}, err
		}
		if step.Assert != "" {
			ok, perr := EvalPredicate(step.Assert, resp)
			if perr != nil {
				return Session{}, perr
			}
			if !ok {
				return Session{}, errs.Auth("AUTH_FAILED",
					fmt.Sprintf("login %s assert %q failed: %s", stepLabel(step, i), step.Assert, bodyExcerpt(resp.Body)))
			}
		}
		for name, expr := range step.Capture {
			captured[name] = evalCapture(expr, resp)
		}
		if step.CookieFromSetCookie {
			if c := parseSetCookies(resp.SetCookies); c != "" {
				sessionCookie = mergeCookieStrings(sessionCookie, c)
			}
		}
	}
	// Verify the flow actually produced its declared captures. A wrong password
	// typically returns 200 with an error body, so gjson yields "" — without this
	// gate we'd persist a dead session and fail the next call confusingly.
	for _, step := range tg.Auth.Flow {
		for name := range step.Capture {
			if captured[name] == "" {
				return Session{}, errs.Auth("AUTH_FAILED",
					fmt.Sprintf("login captured empty %q (wrong credentials or response shape changed)", name))
			}
		}
	}
	return Session{Vars: captured, Cookie: sessionCookie}, nil
}

// stepLabel identifies a flow step in error messages: its name if set, else its
// 1-based position.
func stepLabel(s FlowStep, i int) string {
	if s.Name != "" {
		return fmt.Sprintf("step %q", s.Name)
	}
	return fmt.Sprintf("step %d", i+1)
}

// parseRawHTTP turns "METHOD PATH\nHeader: v\n\nbody" (with {{vars}}) into a CallRequest.
func parseRawHTTP(raw string, vars map[string]string) *CallRequest {
	raw = renderAll(raw, vars)
	head, body, _ := strings.Cut(raw, "\n\n")
	lines := strings.Split(strings.TrimSpace(head), "\n")
	req := &CallRequest{Body: []byte(body)}
	if len(lines) > 0 {
		if m, p, ok := strings.Cut(strings.TrimSpace(lines[0]), " "); ok {
			req.Method, req.Path = m, strings.TrimSpace(p)
		}
		for _, h := range lines[1:] {
			if strings.TrimSpace(h) != "" {
				req.Headers = append(req.Headers, h)
			}
		}
	}
	return req
}

// renderAll substitutes {{var}} tokens (login templates always fully fill).
func renderAll(tpl string, vars map[string]string) string {
	for k, v := range vars {
		tpl = strings.ReplaceAll(tpl, "{{"+k+"}}", v)
	}
	return tpl
}
