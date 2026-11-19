package tcli

import "testing"

func TestResolveAPITarget_SoleApp(t *testing.T) {
	c := &Case{
		Name: "t",
		Apps: map[string]AppDecl{
			"orders": {App: "orders", Actor: Actor{Name: "biz_user"}, BaseURL: "https://biz"},
		},
	}
	// sole app: empty target resolves to the only entry
	app, actor, base, err := resolveAPITarget(c, &APIStep{Request: "GET /x"})
	if err != nil || app != "orders" || actor.Name != "biz_user" || base != "https://biz" {
		t.Fatalf("sole app: app=%s actor=%+v base=%s err=%v", app, actor, base, err)
	}
}

func TestResolveAPITarget_ExplicitTarget(t *testing.T) {
	c := &Case{
		Name: "t",
		Apps: map[string]AppDecl{
			"biz": {App: "biz", Actor: Actor{Name: "biz_user"}, BaseURL: "https://biz"},
			"cfg": {App: "cfg", Actor: Actor{Name: "admin"}, BaseURL: "https://cfg"},
		},
	}
	// explicit target selects the right entry
	app, actor, base, err := resolveAPITarget(c, &APIStep{Target: "cfg", Request: "GET /y"})
	if err != nil || app != "cfg" || actor.Name != "admin" || base != "https://cfg" {
		t.Fatalf("explicit target: app=%s actor=%+v base=%s err=%v", app, actor, base, err)
	}
}

func TestResolveAPITarget_AmbiguousErrors(t *testing.T) {
	c := &Case{
		Name: "t",
		Apps: map[string]AppDecl{
			"a": {App: "a"},
			"b": {App: "b"},
		},
	}
	// two apps, no target -> ambiguous
	_, _, _, err := resolveAPITarget(c, &APIStep{Request: "GET /x"})
	if err == nil {
		t.Fatal("expected error for ambiguous target")
	}
}

func TestResolveAPITarget_AppDefaultsToKey(t *testing.T) {
	c := &Case{
		Name: "t",
		Apps: map[string]AppDecl{
			"orders": {}, // App field empty -> defaults to key "orders"
		},
	}
	app, _, _, err := resolveAPITarget(c, &APIStep{Request: "GET /x"})
	if err != nil || app != "orders" {
		t.Fatalf("app should default to key: app=%s err=%v", app, err)
	}
}

func TestResolveTarget_SoleDB(t *testing.T) {
	m := map[string]string{"main": "orders_uat"}
	target, err := resolveTarget("", m, "db")
	if err != nil || target != "orders_uat" {
		t.Fatalf("sole db: target=%s err=%v", target, err)
	}
}

func TestResolveTarget_Unknown(t *testing.T) {
	m := map[string]string{"main": "orders_uat"}
	_, err := resolveTarget("missing", m, "db")
	if err == nil {
		t.Fatal("expected error for undeclared target")
	}
}

func TestSeedBuiltins(t *testing.T) {
	vars := map[string]string{}
	seedBuiltins(vars, "RID-123")
	if vars["run_id"] != "RID-123" || vars["uuid"] == "" || vars["now"] == "" {
		t.Fatalf("builtins not seeded: %v", vars)
	}
}
