// Package dataease is the dbcli "dataease" driver: a read-only HTTP bypass for
// when a database can't be reached directly but its DataEase web service can. It
// translates a read-only SQL into a DataEase sqlPreview request. It is a
// last-resort path — prefer a direct db driver whenever the network allows.
//
// It does not use sqlcore's execution engine (no auto-LIMIT) and has no write
// path: --allow-write is meaningless here. It DOES reuse sqlcore's statement
// classifier as a local read-only gate (see guardReadOnly) so a write/DDL/
// multi-statement is rejected here, with the shared error codes, before any HTTP
// call — the remote endpoint is not the only line of defense. Transport is
// net/http only.
//
// The `insert` verb renders a SELECT's rows as INSERT statements (read-only,
// like the SQL drivers'), but DataEase returns every value as a string with no
// type info, so values are emitted MySQL-flavored and quoted as strings (NULL
// for null) — see insert.go.
package dataease

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/no-today/aidev-clis/internal/core/config"
	"github.com/no-today/aidev-clis/internal/core/cred"
	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/dbcli"
	"github.com/no-today/aidev-clis/internal/dbcli/sqlcore"
)

type adapter struct{}

func New() dbcli.Driver { return adapter{} }

func (adapter) Name() string { return "dataease" }

func (a adapter) Run(ctx context.Context, in dbcli.Input, out dbcli.Output) error {
	cfg, err := ParseConfig(in.Target.Name, in.Target.Raw)
	if err != nil {
		return err
	}

	// `doctor` is a connectivity probe: ensure a session (auto-login if set) and
	// run a trivial query. The run loop's runDoctor passes a discardOutput, so we
	// signal health purely via the returned error — no Batch.
	if len(in.Args) > 0 && strings.EqualFold(in.Args[0], "doctor") {
		result, err := queryWithAutoLogin(ctx, cfg, "select 1")
		if err != nil {
			return err
		}
		if len(result.Rows) != 1 {
			return errs.General("DATAEASE_DOCTOR_BAD_RESPONSE", "DataEase ping did not return one row")
		}
		return nil
	}

	// `insert` renders a SELECT's rows as INSERT statements via RawOutput.
	if len(in.Args) > 0 && strings.EqualFold(in.Args[0], "insert") {
		return runInsert(ctx, cfg, in, out)
	}

	// dataease is a read-only SELECT bypass: the catalog verbs other drivers
	// expose have no DataEase equivalent. Reject them clearly instead of sending
	// the bare word to sqlPreview as SQL.
	if len(in.Args) > 0 {
		switch strings.ToLower(in.Args[0]) {
		case "databases", "tables", "describe":
			return errs.Config("DATAEASE_UNSUPPORTED_VERB",
				"dataease is a read-only bypass: only a SELECT, `insert`, and `doctor` are supported (no "+strings.ToLower(in.Args[0])+")")
		}
	}

	sqlStr := strings.TrimSpace(strings.Join(in.Args, " "))
	if sqlStr == "" {
		return errs.General("ARG_REQUIRED", "dataease requires a SQL statement")
	}
	if err := guardReadOnly(sqlStr); err != nil {
		return err
	}
	result, err := queryWithAutoLogin(ctx, cfg, sqlStr)
	if err != nil {
		return err
	}
	return out.Batch(*result)
}

// guardReadOnly is dataease's local defense-in-depth: it runs the same sqlcore
// statement classifier the mysql/pg drivers use, so a write, DDL, or smuggled
// multi-statement is rejected here — with the shared error codes — before any
// HTTP call reaches the remote sqlPreview endpoint. DataEase is read-only by
// design and has no --allow-write path, so anything that is not a pure read is
// refused (unknown statements classify as write and thus fail safe).
func guardReadOnly(sqlStr string) error {
	if sqlcore.HasMultipleStatements(sqlStr) {
		return errs.Config("MULTI_STATEMENT", "multiple statements are not allowed; run one statement at a time")
	}
	switch sqlcore.Classify(sqlStr) {
	case sqlcore.ClassRead:
		return nil
	case sqlcore.ClassDDL:
		return errs.Config("DDL_REFUSED", "DDL (CREATE/DROP/ALTER/TRUNCATE/GRANT/REVOKE) is never allowed")
	default:
		return errs.Config("WRITE_NOT_ALLOWED", "dataease is a read-only bypass; only a SELECT (or read-only WITH) is permitted")
	}
}

// sessionsDir is ~/.aidev-clis/sessions (AIDEV_CLIS_HOME-aware).
func sessionsDir() (string, error) {
	home, err := config.Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "sessions"), nil
}

// queryWithAutoLogin loads the session and queries; on a recoverable auth/session
// error it logs in once (when a credential is configured), saves the session, and
// retries the original query exactly once.
func queryWithAutoLogin(ctx context.Context, cfg *Config, sqlStr string) (*dbcli.Result, error) {
	dir, err := sessionsDir()
	if err != nil {
		return nil, err
	}
	client := NewClient(cfg)
	sess, err := LoadSession(cfg.SessionKey, cfg.BaseURL, dir)
	if err != nil {
		if !canAutoLogin(cfg, err) {
			return nil, err
		}
		sess, err = loginAndSaveSession(ctx, client, cfg, dir)
		if err != nil {
			return nil, err
		}
	}
	result, err := client.Query(ctx, sess, sqlStr)
	if err == nil {
		return result, nil
	}
	if !canAutoLogin(cfg, err) {
		return nil, err
	}
	sess, loginErr := loginAndSaveSession(ctx, client, cfg, dir)
	if loginErr != nil {
		return nil, loginErr
	}
	return client.Query(ctx, sess, sqlStr)
}

func loginAndSaveSession(ctx context.Context, client *Client, cfg *Config, dir string) (*Session, error) {
	credData, err := cred.Read(cfg.LoginCredential)
	if err != nil {
		return nil, err
	}
	loginCred, err := ParseLoginCredential(credData)
	if err != nil {
		return nil, err
	}
	sess, err := client.Login(ctx, *loginCred)
	if err != nil {
		return nil, err
	}
	if _, err := SaveSession(sess, cfg.SessionKey, dir); err != nil {
		return nil, err
	}
	return sess, nil
}

// canAutoLogin reports whether err is a session/auth condition worth a single
// re-login. WAF blocks and generic remote/HTTP errors are deliberately excluded.
func canAutoLogin(cfg *Config, err error) bool {
	if cfg == nil || cfg.LoginCredential == "" || err == nil {
		return false
	}
	var e *errs.Error
	if !errors.As(err, &e) {
		return false
	}
	switch e.Code {
	case "DATAEASE_SESSION_MISSING",
		"DATAEASE_SESSION_NO_TOKEN",
		"DATAEASE_SESSION_INVALID_JSON",
		"DATAEASE_AUTH_EXPIRED":
		return true
	default:
		return false
	}
}
