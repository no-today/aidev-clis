package tcli

import (
	"context"
	"testing"
)

// fakeRunner:按 (name + 首个 arg) 路由返回预制 stdout/err。后续任务复用。
// queues 提供按键的响应队列(依次弹出);队列耗尽后回退 responses/errs 静态值。
type fakeRunner struct {
	responses map[string][]byte // key: name+" "+args[0]
	errs      map[string]error
	queues    map[string][][]byte // key: name+" "+args[0]; 每次调用弹第一个
	calls     [][]string          // 记录每次 args,供断言
}

func (f *fakeRunner) Run(_ context.Context, name string, args, _ []string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	key := name
	if len(args) > 0 {
		key = name + " " + args[0]
	}
	if f.queues != nil {
		if q, ok := f.queues[key]; ok && len(q) > 0 {
			resp := q[0]
			f.queues[key] = q[1:]
			return resp, nil
		}
	}
	return f.responses[key], f.errs[key]
}

func TestDiscover_DBDriverCached(t *testing.T) {
	f := &fakeRunner{responses: map[string][]byte{
		"dbcli targets": []byte(`{"data":[{"name":"orders_uat","adapter":"mysql"},{"name":"ledger","adapter":"postgres"}]}`),
	}}
	d := newDiscoverer(f, nil)
	drv, err := d.dbDriver(context.Background(), "orders_uat")
	if err != nil || drv != "mysql" {
		t.Fatalf("got %q err %v", drv, err)
	}
	// 第二次不应再跑 dbcli targets
	_, _ = d.dbDriver(context.Background(), "ledger")
	n := 0
	for _, c := range f.calls {
		if c[0] == "dbcli" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("dbcli targets ran %d times, want 1 (cached)", n)
	}
}

func TestDiscover_LogAdapterUnknownEnv(t *testing.T) {
	f := &fakeRunner{responses: map[string][]byte{
		"logcli targets": []byte(`{"data":[{"name":"orders_sls","adapter":"sls"}]}`),
	}}
	d := newDiscoverer(f, nil)
	if _, err := d.logAdapter(context.Background(), "missing"); err == nil {
		t.Fatal("expected error for unknown log env")
	}
}
