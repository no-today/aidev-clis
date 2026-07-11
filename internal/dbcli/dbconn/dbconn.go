// Package dbconn resolves a connectable DSN for a dbcli env: it injects the
// password from the credstore into the URL userinfo and (in 3a-2) opens an SSH
// tunnel, rewriting the host to the local forward. Transport only — it knows
// nothing about SQL.
package dbconn

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/no-today/aidev-clis/internal/core/config"
	"github.com/no-today/aidev-clis/internal/core/cred"
	"github.com/no-today/aidev-clis/internal/core/diag"
	"github.com/no-today/aidev-clis/internal/core/errs"
)

// Redact removes u's password from a driver error/message string so a
// connection error can never surface the credential in AI-facing output. Belt
// to the drivers' own (inconsistent) redaction.
func Redact(msg string, u *url.URL) string {
	if u != nil && u.User != nil {
		if pw, ok := u.User.Password(); ok && pw != "" {
			msg = strings.ReplaceAll(msg, pw, "xxxxx")
		}
	}
	return msg
}

// RedactErr scrubs u's password from a returned error's message (in place for an
// *errs.Error, otherwise by rewrapping). Drivers wrap connection failures with
// the raw DSN; the caller holds u, so it redacts at the boundary. Returns nil
// for nil.
func RedactErr(err error, u *url.URL) error {
	if err == nil {
		return nil
	}
	if e, ok := err.(*errs.Error); ok {
		e.Message = Redact(e.Message, u)
		return e
	}
	return errs.General("UNKNOWN", Redact(err.Error(), u))
}

// trimCred drops a trailing newline from a credential file's bytes — credential
// files are commonly created with a trailing `\n` (e.g. `echo pw > file`), which
// must not become part of the password/passphrase. Only `\r`/`\n` are trimmed, so
// a password with legitimate leading/trailing spaces is preserved.
func trimCred(b []byte) string { return strings.TrimRight(string(b), "\r\n") }

// Resolve returns the target's DSN as a *url.URL with the password injected, plus a
// cleanup func (closes any tunnel; a no-op in 3a-1). The AI never sees the URL.
func Resolve(ctx context.Context, target config.Target) (*url.URL, func(), error) {
	dsn, _ := target.Raw["dsn"].(string)
	if dsn == "" {
		return nil, nil, errs.Config("DSN_MISSING", "target block missing 'dsn'")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, nil, errs.Config("DSN_INVALID", fmt.Sprintf("cannot parse dsn: %v", err))
	}
	credName, _ := target.Raw["credential"].(string)
	if credName != "" {
		pw, err := cred.Read(credName)
		if err != nil {
			return nil, nil, err
		}
		u.User = url.UserPassword(u.User.Username(), trimCred(pw))
	}
	if sshRaw, ok := target.Raw["ssh"].(map[string]any); ok {
		sshCfg, err := ParseSSHConfig(sshRaw)
		if err != nil {
			return nil, nil, err
		}
		if sshCfg.IdentityFile != "" && sshCfg.KeyPassphraseCredential != "" {
			pp, err := cred.Read(sshCfg.KeyPassphraseCredential)
			if err != nil {
				return nil, nil, err
			}
			sshCfg.Passphrase = trimCred(pp)
		}
		if sshCfg.PasswordCredential != "" {
			pw, err := cred.Read(sshCfg.PasswordCredential)
			if err != nil {
				return nil, nil, err
			}
			sshCfg.Password = trimCred(pw)
		}
		host := u.Hostname()
		port := defaultPort(u.Scheme)
		if ps := u.Port(); ps != "" {
			port, _ = strconv.Atoi(ps)
		}
		tunStart := time.Now()
		tun, err := OpenTunnel(ctx, *sshCfg, host, port)
		if err != nil {
			return nil, nil, err
		}
		diag.From(ctx).Logf(1, "ssh tunnel opened in %s: %s@%s:%d → %s:%d",
			time.Since(tunStart).Round(time.Microsecond), sshCfg.User, sshCfg.Host, sshCfg.Port, host, port)
		u.Host = tun.LocalAddr() // route the DSN through the local forward
		return u, func() { _ = tun.Close() }, nil
	}
	return u, func() {}, nil
}

func defaultPort(scheme string) int {
	switch scheme {
	case "mysql":
		return 3306
	case "postgres", "postgresql":
		return 5432
	case "redis", "rediss":
		return 6379
	}
	return 0
}
