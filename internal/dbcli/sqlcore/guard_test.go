package sqlcore

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		sql  string
		want Class
	}{
		{"SELECT * FROM t", ClassRead},
		{"  select 1", ClassRead},
		{"SHOW TABLES", ClassRead},
		{"EXPLAIN SELECT 1", ClassRead},
		{"WITH x AS (SELECT 1) SELECT * FROM x", ClassRead},
		{"INSERT INTO t VALUES (1)", ClassWrite},
		{"UPDATE t SET a=1", ClassWrite},
		{"DELETE FROM t", ClassWrite},
		{"CREATE TABLE t (id int)", ClassDDL},
		{"DROP TABLE t", ClassDDL},
		{"ALTER TABLE t ADD c int", ClassDDL},
		{"TRUNCATE t", ClassDDL},
		{"GRANT ALL ON t TO u", ClassDDL},
		// INTO OUTFILE is a server-side file write → unconditional-refuse tier
		{"SELECT * INTO outfile '/tmp/x' FROM t", ClassHazard},
		// pseudo-readonly downgrades → write
		{"SELECT * FROM t FOR UPDATE", ClassWrite},
		{"SELECT nextval('s')", ClassWrite},
		{"SELECT pg_sleep(10)", ClassWrite},
		{"SELECT load_file('/etc/passwd')", ClassWrite},
		{"EXPLAIN ANALYZE SELECT 1", ClassWrite},
		{"WITH d AS (DELETE FROM t RETURNING *) SELECT * FROM d", ClassWrite},
		// keyword inside a string literal must NOT trip classification
		{"SELECT 'drop table x' AS note", ClassRead},
		{"SELECT * FROM t WHERE c = 'a; DROP TABLE t'", ClassRead},
		// unknown verb → write (needs --allow-write, but not DDL-refused)
		{"SET autocommit=1", ClassWrite},
	}
	for _, c := range cases {
		if got := Classify(c.sql); got != c.want {
			t.Errorf("Classify(%q) = %v, want %v", c.sql, got, c.want)
		}
	}
}

func TestHasMultipleStatements(t *testing.T) {
	if !HasMultipleStatements("SELECT 1; DROP TABLE t") {
		t.Error("two statements must be detected")
	}
	if HasMultipleStatements("SELECT 1;") {
		t.Error("a single trailing semicolon is not multiple statements")
	}
	if HasMultipleStatements("SELECT ';' AS x") {
		t.Error("semicolon inside a literal is not a separator")
	}
}

// EXPLAIN plans without executing: the explained statement's keywords must not
// classify it as a write (EXPLAIN <INSERT...> is the read-only way to validate
// a write script). EXPLAIN ANALYZE executes and stays a write.
func TestClassify_ExplainIsReadEvenForWrites(t *testing.T) {
	reads := []string{
		"EXPLAIN INSERT INTO t (a) VALUES (1)",
		"EXPLAIN UPDATE t SET a=1",
		"explain select * from t for update",
		"EXPLAIN (FORMAT JSON) INSERT INTO t SELECT * FROM s",
	}
	for _, q := range reads {
		if got := Classify(q); got != ClassRead {
			t.Errorf("Classify(%q) = %v, want ClassRead", q, got)
		}
	}
	if got := Classify("EXPLAIN ANALYZE INSERT INTO t (a) VALUES (1)"); got != ClassWrite {
		t.Errorf("EXPLAIN ANALYZE must stay ClassWrite, got %v", got)
	}
}
