package tcli

import "testing"

func TestActorUnmarshal_Named(t *testing.T) {
	y := `name: t
apps:
  orders:
    actor: qa_buyer
steps:
  - name: s
    api:
      request: "GET /x"
`
	c, err := parseCaseBytes([]byte(y), "t.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if c.Apps["orders"].Actor.Name != "qa_buyer" || c.Apps["orders"].Actor.Vars != nil {
		t.Fatalf("named actor parse: %+v", c.Apps["orders"].Actor)
	}
}

func TestActorUnmarshal_Inline(t *testing.T) {
	y := `name: t
apps:
  orders:
    actor:
      vars: {username: u, password: p}
steps:
  - name: s
    api:
      request: "GET /x"
`
	c, err := parseCaseBytes([]byte(y), "t.yaml")
	if err != nil {
		t.Fatal(err)
	}
	actor := c.Apps["orders"].Actor
	if actor.Name != "" || actor.Vars["username"] != "u" {
		t.Fatalf("inline actor parse: %+v", actor)
	}
}

func TestValidate_WriteNeedsSafety(t *testing.T) {
	y := `name: t
dbs:
  main: orders_uat
steps:
  - name: w
    db:
      sql: "DELETE FROM x"
      write: true
`
	c, _ := parseCaseBytes([]byte(y), "t.yaml")
	diags := c.Validate()
	if len(diags) == 0 {
		t.Fatal("expected SAFETY diagnostic for write without allow_db_write")
	}
}

func TestValidate_ActionExactlyOne(t *testing.T) {
	y := `name: t
steps:
  - name: bad
`
	c, _ := parseCaseBytes([]byte(y), "t.yaml")
	if len(c.Validate()) == 0 {
		t.Fatal("expected diagnostic for action with no api/db/log")
	}
}

func TestValidate_NoNameDiag(t *testing.T) {
	y := `steps:
  - name: s
    api:
      request: "GET /x"
`
	c, _ := parseCaseBytes([]byte(y), "t.yaml")
	diags := c.Validate()
	found := false
	for _, d := range diags {
		if d == "case has no name" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 'case has no name', got %v", diags)
	}
}

func TestValidate_BadExpr(t *testing.T) {
	y := `name: t
apps:
  orders: {}
steps:
  - name: s
    api:
      request: "GET /x"
      expect:
        - "status_code BADOP 200"
`
	c, _ := parseCaseBytes([]byte(y), "t.yaml")
	diags := c.Validate()
	if len(diags) == 0 {
		t.Fatalf("expected diagnostic for bad expression, got none")
	}
}

func TestTagMatch(t *testing.T) {
	c := &Case{Tags: []string{"Smoke", "Checkout"}}
	if !c.TagMatch(nil) {
		t.Fatal("empty want must always match")
	}
	if !c.TagMatch([]string{"smoke"}) {
		t.Fatal("case-insensitive single tag should match")
	}
	if !c.TagMatch([]string{"smoke", "checkout"}) {
		t.Fatal("all wanted tags present (AND) should match")
	}
	if c.TagMatch([]string{"smoke", "regression"}) {
		t.Fatal("missing one wanted tag (AND) must not match")
	}
	if c.TagMatch([]string{"regression"}) {
		t.Fatal("unrelated tag must not match")
	}
}
