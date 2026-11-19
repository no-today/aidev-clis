package mongo

import "testing"

func TestParseStatement_FindWithChain(t *testing.T) {
	st, err := ParseStatement(`db.users.find({status:"active"}, {name:1}).limit(50).sort({age:-1}).skip(10)`)
	if err != nil {
		t.Fatal(err)
	}
	if st.Collection != "users" || st.Method != "find" {
		t.Fatalf("coll/method: %+v", st)
	}
	if len(st.Args) != 2 {
		t.Fatalf("want 2 args, got %d", len(st.Args))
	}
	if len(st.Modifiers) != 3 || st.Modifiers[0].Name != "limit" {
		t.Fatalf("modifiers: %+v", st.Modifiers)
	}
}

func TestParseStatement_Forms(t *testing.T) {
	for _, in := range []string{
		`db.orders.aggregate([{$group:{_id:"$s"}}])`,
		`db.users.countDocuments({})`,
		`db.getCollection("weird.name").find({})`,
		`db["dash-coll"].deleteOne({_id:1})`,
		`db.users.find()`,
	} {
		if _, err := ParseStatement(in); err != nil {
			t.Errorf("ParseStatement(%q): %v", in, err)
		}
	}
}

func TestParseStatement_Malformed(t *testing.T) {
	for _, bad := range []string{
		`users.find({})`, `db.find({})`, `db.users.`, `db.users.find`,
		`db.users.find({}`, `db.users.find({}).limit`, ``, `db..find({})`,
	} {
		if _, err := ParseStatement(bad); err == nil {
			t.Errorf("ParseStatement(%q) should error", bad)
		}
	}
}

// Extra statement coverage beyond the plan.
func TestParseStatement_Extra(t *testing.T) {
	// findOne with no args
	st, err := ParseStatement(`db.users.findOne()`)
	if err != nil {
		t.Fatal(err)
	}
	if st.Method != "findOne" || len(st.Args) != 0 {
		t.Fatalf("findOne no-args: %+v", st)
	}
	// aggregate with $merge (write detection)
	st2, err := ParseStatement(`db.orders.aggregate([{$group:{_id:"$s"}},{$merge:"archive"}])`)
	if err != nil {
		t.Fatal(err)
	}
	if st2.Method != "aggregate" || len(st2.Args) != 1 {
		t.Fatalf("aggregate args: %+v", st2)
	}
	if !AggregateWrites(st2.Args[0]) {
		t.Fatal("$merge pipeline must be classified as write")
	}
	// multiple chained modifiers
	st3, err := ParseStatement(`db.logs.find({level:"error"}).sort({ts:-1}).skip(0).limit(100)`)
	if err != nil {
		t.Fatal(err)
	}
	if len(st3.Modifiers) != 3 {
		t.Fatalf("want 3 modifiers, got %d", len(st3.Modifiers))
	}
}
