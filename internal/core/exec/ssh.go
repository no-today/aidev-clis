package exec

import (
	"context"
	"strconv"
	"strings"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// SSH runs the Spec on a remote host via the system `ssh`. It reuses Local to
// run the assembled `ssh ...` command (so streaming/exit handling is shared).
type SSH struct {
	Host         string
	User         string
	Port         int    // 0 -> default 22 (omit -p)
	IdentityFile string // optional
}

// validate rejects host/user values that ssh would reparse as something other
// than a destination — an embedded '@' confuses ssh's own user@host split, and
// a leading '-' turns the token into an option (e.g. a user of
// "-oProxyCommand=..." would run a local command). Config is operator-trusted,
// but this is a cheap belt for the credential-hiding tool's own transport.
func (s SSH) validate() error {
	for label, v := range map[string]string{"host": s.Host, "user": s.User} {
		if strings.ContainsAny(v, "@") || strings.HasPrefix(v, "-") {
			return errs.Config("SSH_BAD_TARGET",
				"ssh "+label+" must not contain '@' or start with '-': "+v)
		}
	}
	return nil
}

// sshArgv assembles the local `ssh ... user@host <remote command>` command.
func (s SSH) sshArgv(spec Spec) []string {
	argv := []string{"ssh", "-o", "BatchMode=yes"}
	if s.IdentityFile != "" {
		argv = append(argv, "-i", s.IdentityFile)
	}
	if s.Port != 0 { // 0 means "let ssh use its own default (22)"
		argv = append(argv, "-p", strconv.Itoa(s.Port))
	}
	// The LOCAL ssh invocation is argv-safe (Local exec passes argv to the
	// kernel, no local shell). But the REMOTE sshd runs whatever follows the
	// host through the login shell (`sh -c "<joined>"`), so pass-through tokens
	// (container names, --tail values) would otherwise inject remote commands.
	// We therefore send ONE pre-quoted command string instead of relying on
	// ssh's own space-join of the remaining argv.
	//
	// Env is applied to the REMOTE command via an `env` prefix, so SSH.Run's
	// Env means the same thing as Local.Run's Env (the run command's
	// environment) — sending it as ssh-client env would silently not reach the
	// remote process. `env` is POSIX and treats each 'K=V' operand as an
	// assignment even when quoted with embedded spaces.
	remote := shellQuoteArgs(spec.Argv)
	if len(spec.Env) > 0 {
		remote = "env " + shellQuoteArgs(spec.Env) + " " + remote
	}
	// No trailing "--": the remote command is always single-quoted (starts with
	// "'", never "-"), so ssh can't misparse it as an option, and a literal "--"
	// is passed through to the remote shell by pre-8.5 ssh clients as a bogus
	// first word ("--: command not found").
	argv = append(argv, s.User+"@"+s.Host, remote)
	return argv
}

// shellQuoteArgs single-quotes each argument (escaping any embedded single
// quote as '\”) and joins them with spaces, producing a command string that
// the remote shell executes verbatim with no metacharacter interpretation.
func shellQuoteArgs(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(quoted, " ")
}

func (s SSH) Run(ctx context.Context, spec Spec, onLine func(string) error) error {
	if err := s.validate(); err != nil {
		return err
	}
	// spec.Env is folded into the remote command by sshArgv, NOT passed to the
	// local ssh client (which would set it on ssh itself, never the remote
	// process). The local ssh inherits only the ambient environment.
	return Local{}.Run(ctx, Spec{Argv: s.sshArgv(spec)}, onLine)
}
