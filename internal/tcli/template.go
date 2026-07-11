package tcli

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

var tmplRe = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_]+)\s*\}\}`)

// Render 替换 s 中所有 {{name}} 为 vars[name];任一缺失返回 TEMPLATE_VAR_MISSING。
func Render(s string, vars map[string]string) (string, error) {
	var missing string
	out := tmplRe.ReplaceAllStringFunc(s, func(m string) string {
		name := strings.TrimSpace(tmplRe.FindStringSubmatch(m)[1])
		v, ok := vars[name]
		if !ok {
			if missing == "" {
				missing = name
			}
			return m
		}
		return v
	})
	if missing != "" {
		return "", errs.General("TEMPLATE_VAR_MISSING", fmt.Sprintf("undefined variable {{%s}}", missing))
	}
	return out, nil
}

// RenderMap 对 map 的每个 value 调用 Render。
func RenderMap(m, vars map[string]string) (map[string]string, error) {
	if m == nil {
		return nil, nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		r, err := Render(v, vars)
		if err != nil {
			return nil, err
		}
		out[k] = r
	}
	return out, nil
}

// TemplateVars 返回 s 中引用的所有变量名(供 validate 静态检测,见 Task 11)。
func TemplateVars(s string) []string {
	var names []string
	for _, m := range tmplRe.FindAllStringSubmatch(s, -1) {
		names = append(names, strings.TrimSpace(m[1]))
	}
	return names
}
