package tcli

import (
	"reflect"
	"testing"
)

func TestActorHash_Stable(t *testing.T) {
	h1 := actorHash(map[string]string{"u": "a", "p": "b"})
	h2 := actorHash(map[string]string{"p": "b", "u": "a"}) // order-independent
	if h1 != h2 || len(h1) != 8 {
		t.Fatalf("hash not stable/8-char: %q %q", h1, h2)
	}
}

func TestAPIArgs_NamedActorWithBody(t *testing.T) {
	// v2: apiArgs(app, method, path, headers, body, act, baseURL, saveBody, timeout)
	got := apiArgs("orders", "POST", "/orders", []string{"X-A: b"}, `{"k":1}`,
		resolvedActor{Name: "qa_buyer"}, "https://uat", "", "")
	want := []string{"call", "orders", "/orders", "-X", "POST",
		"-H", "X-A: b", "-d", `{"k":1}`,
		"--actor", "qa_buyer", "--base-url", "https://uat", "--output", "json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("\n got=%v\nwant=%v", got, want)
	}
}

func TestAPIArgs_InlineActorFileAndSaveBody(t *testing.T) {
	got := apiArgs("orders", "GET", "/x", nil, "",
		resolvedActor{Name: "inline-abc12345", FilePath: "/tmp/a.yaml"}, "", "/tmp/o.xlsx", "")
	want := []string{"call", "orders", "/x", "-X", "GET",
		"--actor", "inline-abc12345", "--actor-file", "/tmp/a.yaml",
		"--output-file", "/tmp/o.xlsx", "--output", "json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("\n got=%v\nwant=%v", got, want)
	}
}

func TestAPIArgs_DefaultMethodGET(t *testing.T) {
	got := apiArgs("svc", "", "/ping", nil, "", resolvedActor{}, "", "", "")
	if len(got) < 5 || got[3] != "-X" || got[4] != "GET" {
		t.Fatalf("empty method should default to GET: %v", got)
	}
}

func TestAPIArgs_TimeoutFlag(t *testing.T) {
	got := apiArgs("svc", "GET", "/x", nil, "", resolvedActor{}, "", "", "10s")
	found := false
	for i, a := range got {
		if a == "--connect-timeout" && i+1 < len(got) && got[i+1] == "10s" {
			found = true
		}
	}
	if !found {
		t.Fatalf("timeout flag missing: %v", got)
	}
}

func TestDBArgs_WriteAndTarget(t *testing.T) {
	got := dbArgs("mysql", "orders_uat", "DELETE FROM x", true, "10s")
	want := []string{"mysql", "--target", "orders_uat", "--allow-write", "--timeout", "10s", "DELETE FROM x"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("\n got=%v\nwant=%v", got, want)
	}
}

func TestLogArgs_SLSTraceVsNonSLS(t *testing.T) {
	tr := logArgs("sls", "orders_sls", &LogStep{Trace: "abc", From: "5m", Size: 200}, "abc")
	wantTr := []string{"sls", "--target", "orders_sls", "trace", "abc", "--from", "5m", "--size", "200"}
	if !reflect.DeepEqual(tr, wantTr) {
		t.Fatalf("sls trace argv\n got=%v\nwant=%v", tr, wantTr)
	}
	nf := logArgs("kubectl", "uat_k8s", &LogStep{Trace: "abc", Size: 300}, "abc")
	wantNF := []string{"kubectl", "--target", "uat_k8s", "logs", "--tail", "300"}
	if !reflect.DeepEqual(nf, wantNF) {
		t.Fatalf("non-sls logs argv\n got=%v\nwant=%v", nf, wantNF)
	}
}
