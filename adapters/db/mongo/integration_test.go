//go:build integration

package mongo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/no-today/aidev-clis/internal/dbcli"
)

func writeEnv(t *testing.T) {
	t.Helper()
	if c, err := net.DialTimeout("tcp", "127.0.0.1:27017", time.Second); err != nil {
		t.Skip("mongo not reachable on 127.0.0.1:27017; run make db-up")
	} else {
		_ = c.Close()
	}
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	cfg := "default_target: it\ntargets:\n  it:\n    adapter: mongo\n    dsn: mongodb://127.0.0.1:27017/itdb\n"
	if err := os.WriteFile(filepath.Join(home, "dbcli.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, allowWrite bool, args ...string) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	reg := dbcli.NewRegistry([]dbcli.Driver{New()})
	dbcli.Run(context.Background(), reg, dbcli.RunArgs{Driver: "mongo", Target: "it", AllowWrite: allowWrite, Args: args, Out: &buf})
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("bad envelope: %s", buf.String())
	}
	return env
}

func TestIT_Mongo_RoundTrip(t *testing.T) {
	writeEnv(t)
	_ = run(t, true, `db.users.deleteMany({})`) // clean slate
	if run(t, true, `db.users.insertOne({_id:1,name:"alice",age:30})`)["data"].(map[string]any)["affected"].(float64) != 1 {
		t.Fatal("insertOne")
	}
	_ = run(t, true, `db.users.insertOne({_id:2,name:"bob",age:25})`)

	docs := run(t, false, `db.users.find({age:{$gt:20}}).sort({age:-1})`)["data"].([]any)
	if len(docs) != 2 || docs[0].(map[string]any)["name"] != "alice" {
		t.Fatalf("find: %v", docs)
	}
	if run(t, false, `db.users.countDocuments({})`)["data"].(map[string]any)["count"].(float64) != 2 {
		t.Fatal("count")
	}
	if run(t, false, `db.users.deleteOne({_id:1})`)["error"].(map[string]any)["code"] != "WRITE_NOT_ALLOWED" {
		t.Fatal("want WRITE_NOT_ALLOWED")
	}
	if run(t, true, `db.users.drop()`)["error"].(map[string]any)["code"] != "MONGO_METHOD_REFUSED" {
		t.Fatal("want MONGO_METHOD_REFUSED")
	}
	if len(run(t, false, "tables")["data"].(map[string]any)["rows"].([]any)) < 1 {
		t.Fatal("tables")
	}
	dchecks := run(t, false, "doctor")["data"].([]any)
	if dchecks[len(dchecks)-1].(map[string]any)["ok"] != true {
		t.Fatal("doctor")
	}
	_ = run(t, true, `db.users.deleteMany({})`)
}

// TestIT_Mongo_ReadCaps proves aggregate and distinct bound their output to the
// default 100-doc cap and emit a truncation warning, matching find.
func TestIT_Mongo_ReadCaps(t *testing.T) {
	writeEnv(t)
	_ = run(t, true, `db.nums.deleteMany({})`)
	// 150 docs, each with a unique n → both aggregate and distinct exceed 100.
	docs := "["
	for i := 0; i < 150; i++ {
		if i > 0 {
			docs += ","
		}
		docs += fmt.Sprintf(`{n:%d}`, i)
	}
	docs += "]"
	_ = run(t, true, `db.nums.insertMany(`+docs+`)`)

	agg := run(t, false, `db.nums.aggregate([{$project:{n:1}}])`)
	if got := len(agg["data"].([]any)); got != 100 {
		t.Fatalf("aggregate not capped at 100: got %d", got)
	}
	if _, ok := agg["warnings"]; !ok {
		t.Fatalf("aggregate over cap must warn: %v", agg)
	}

	dist := run(t, false, `db.nums.distinct("n")`)
	if got := len(dist["data"].([]any)); got != 100 {
		t.Fatalf("distinct not capped at 100: got %d", got)
	}
	if _, ok := dist["warnings"]; !ok {
		t.Fatalf("distinct over cap must warn: %v", dist)
	}

	_ = run(t, true, `db.nums.deleteMany({})`)
}
