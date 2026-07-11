package tcli

import (
	"context"
	"runtime"
	"testing"
)

func TestRunScriptAssert_PassFail(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	// 通过:exit 0
	if f := runScriptAssert(context.Background(), &ScriptAssert{Command: "true"}, map[string]string{}); f != nil {
		t.Fatalf("true should pass: %+v", f)
	}
	// 失败:exit 1
	f := runScriptAssert(context.Background(), &ScriptAssert{Command: "false"}, map[string]string{})
	if f == nil || f.Category != "assertion_failed" {
		t.Fatalf("false should fail assertion: %+v", f)
	}
	// 模板渲染进 args
	if f := runScriptAssert(context.Background(), &ScriptAssert{Command: "test", Args: []string{"{{a}}", "=", "x"}},
		map[string]string{"a": "x"}); f != nil {
		t.Fatalf("templated args should pass: %+v", f)
	}
}
