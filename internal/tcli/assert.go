package tcli

import (
	"context"
	osexec "os/exec"
	"time"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// runScriptAssert 跑 assert_script(渲染 command+args、带超时)。exit!=0 -> assertion 失败。
func runScriptAssert(ctx context.Context, sa *ScriptAssert, vars map[string]string) *Failure {
	if sa == nil {
		return nil
	}
	cmd, err := Render(sa.Command, vars)
	if err != nil {
		return &Failure{Category: "template_failed", Code: errs.From(err).Code, Message: errs.From(err).Message}
	}
	args := make([]string, 0, len(sa.Args))
	for _, a := range sa.Args {
		r, err := Render(a, vars)
		if err != nil {
			return &Failure{Category: "template_failed", Code: errs.From(err).Code, Message: errs.From(err).Message}
		}
		args = append(args, r)
	}
	if sa.Timeout != "" {
		if d, err := time.ParseDuration(sa.Timeout); err == nil {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, d)
			defer cancel()
		}
	}
	out, err := osexec.CommandContext(ctx, cmd, args...).CombinedOutput()
	if err != nil {
		return &Failure{Category: "assertion_failed", Code: "ASSERT_SCRIPT_FAILED",
			Message: cmd + ": " + err.Error() + ": " + truncateStr(string(out), 300)}
	}
	return nil
}
