package tcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/core/exec"
)

// CLIRunner 执行 name(apicli/dbcli/logcli) + args,注入 env(KEY=VALUE)。
// 返回收全的 stdout 与 exec 错误(非 0 退出会有 err,但 stdout 仍有效)。
type CLIRunner interface {
	Run(ctx context.Context, name string, args, env []string) (stdout []byte, runErr error)
}

// cliResult 是 parseEnvelope 的结果。
type cliResult struct {
	HasData bool
	Data    []byte // 信封 data 字段的原始 JSON(HasData 时)
	ErrCode string // 信封 error.code(无 data 时)
	ErrMsg  string
	Raw     []byte // 完整 stdout(脱敏快照用)
}

// localCLI 是生产 CLIRunner:用 exec.Local 跑 sibling 二进制,累积 stdout。
type localCLI struct{}

func (localCLI) Run(ctx context.Context, name string, args, env []string) ([]byte, error) {
	bin := resolveSiblingCLI(name)
	var buf bytes.Buffer
	err := exec.Local{}.Run(ctx, exec.Spec{
		Argv: append([]string{bin}, args...),
		Env:  env,
	}, func(line string) error {
		buf.WriteString(line)
		buf.WriteByte('\n')
		return nil
	})
	return buf.Bytes(), err
}

// resolveSiblingCLI 优先返回与 tcli 同目录的二进制绝对路径,否则回退裸名(走 PATH)。
func resolveSiblingCLI(name string) string {
	exe, err := os.Executable()
	if err != nil {
		return name
	}
	for _, cand := range siblingCandidates(filepath.Dir(exe), name, runtime.GOOS) {
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
	}
	return name
}

// siblingCandidates lists the platform-correct sibling binary paths to try, in
// order. On Windows the executable suffix is required (dbcli.exe next to
// tcli.exe), so we try "<name>.exe" first and fall back to the bare name; on
// POSIX the bare name is canonical. goos is a parameter so the suffix logic is
// unit-testable without cross-compiling.
func siblingCandidates(dir, name, goos string) []string {
	if goos == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		return []string{filepath.Join(dir, name+".exe"), filepath.Join(dir, name)}
	}
	return []string{filepath.Join(dir, name)}
}

// parseEnvelope 解析子进程 stdout。以信封为准:有 data -> HasData(即使 runErr 非 nil);
// 有 error -> 填 ErrCode/ErrMsg;都没有 -> 返回 runErr(或 UNEXPECTED)。
func parseEnvelope(stdout []byte, runErr error) (cliResult, error) {
	res := cliResult{Raw: stdout}
	var env struct {
		Data  json.RawMessage `json:"data"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout), &env); err != nil {
		if runErr != nil {
			return res, errs.From(runErr)
		}
		return res, errs.General("CLI_UNEXPECTED",
			fmt.Sprintf("subprocess produced unparseable output: %s", truncateStr(string(stdout), 300)))
	}
	if env.Data != nil {
		res.HasData = true
		res.Data = env.Data
		return res, nil
	}
	if env.Error != nil {
		res.ErrCode = env.Error.Code
		res.ErrMsg = env.Error.Message
		return res, nil
	}
	if runErr != nil {
		return res, errs.From(runErr)
	}
	return res, errs.General("CLI_UNEXPECTED", "subprocess output had neither data nor error")
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
