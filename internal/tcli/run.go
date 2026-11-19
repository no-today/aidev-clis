package tcli

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// DiscoverCases 把一个文件或目录展开成 case 文件路径列表(目录:*.yaml/*.yml,排序,非递归)。
func DiscoverCases(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, errs.Config("PATH_NOT_FOUND", err.Error())
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, errs.Config("DIR_READ", err.Error())
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if n := e.Name(); strings.HasSuffix(n, ".yaml") || strings.HasSuffix(n, ".yml") {
			out = append(out, filepath.Join(path, e.Name()))
		}
	}
	sort.Strings(out)
	return out, nil
}

// RunCases 解析路径下的 case、按 tag 过滤、用 r 跑,返回 (payload, exitCode)。
// 单文件 -> CaseResult;目录 -> SuiteResult。payload 总是 {data} 信封的内容。
func RunCases(ctx context.Context, r *Runner, path string, tags []string) (any, int, error) {
	// inline actor 落临时目录;用完即清(defer 保证 panic 也能清)。
	defer func() {
		if r.tmpDir != "" {
			_ = os.RemoveAll(r.tmpDir)
		}
	}()
	paths, err := DiscoverCases(path)
	if err != nil {
		return nil, errs.ExitConfig, err
	}
	info, _ := os.Stat(path)
	var results []CaseResult
	for _, p := range paths {
		c, err := ParseCase(p)
		if err != nil {
			return nil, errs.ExitConfig, err
		}
		if !c.TagMatch(tags) {
			continue
		}
		results = append(results, r.RunCase(ctx, c))
	}
	if info != nil && !info.IsDir() {
		if len(results) == 0 {
			return nil, errs.ExitConfig, errs.Config("NO_CASE", "case did not match tag filter")
		}
		cr := results[0]
		return cr, verdictExit(cr.Verdict), nil
	}
	// Directory mode: an empty result set must NOT become a PASS suite (that would
	// let a CI gate go green while verifying nothing). Return NO_CASE (exit 2),
	// distinguishing an empty directory from a tag filter that excluded everything.
	if len(results) == 0 {
		msg := "directory contains no cases"
		if len(paths) > 0 {
			msg = "no cases matched tag filter"
		}
		return nil, errs.ExitConfig, errs.Config("NO_CASE", msg)
	}
	sr := BuildSuiteResult(r.runID, results)
	return sr, verdictExit(sr.Verdict), nil
}

// ValidateCases 解析 + 静态校验(无远程)。返回 {valid, diagnostics, cases} 与退出码。
func ValidateCases(path string, tags []string) (any, int, error) {
	paths, err := DiscoverCases(path)
	if err != nil {
		return nil, errs.ExitConfig, err
	}
	type vrep struct {
		Path        string   `json:"path"`
		Name        string   `json:"name"`
		Valid       bool     `json:"valid"`
		Diagnostics []string `json:"diagnostics,omitempty"`
	}
	var reps []vrep
	allValid := true
	for _, p := range paths {
		c, err := ParseCase(p)
		if err != nil {
			reps = append(reps, vrep{Path: p, Valid: false, Diagnostics: []string{errs.From(err).Message}})
			allValid = false
			continue
		}
		if !c.TagMatch(tags) {
			continue
		}
		d := c.Validate()
		if len(d) > 0 {
			allValid = false
		}
		reps = append(reps, vrep{Path: p, Name: c.Name, Valid: len(d) == 0, Diagnostics: d})
	}
	exit := errs.ExitOK
	if !allValid {
		exit = errs.ExitConfig
	}
	return map[string]any{"valid": allValid, "cases": reps}, exit, nil
}
