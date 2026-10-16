package postgres

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindTransactionControlRejectsTopLevelStatements(t *testing.T) {
	for _, tc := range []struct {
		sql, statement string
		line           int
	}{
		{"CREATE TABLE a(id int);\nCOMMIT;\nCREATE TABLE b(id int);", "COMMIT", 2},
		{"BEGIN;", "BEGIN", 1},
		{"begin transaction isolation level serializable;", "BEGIN", 1},
		{"START TRANSACTION;", "START", 1},
		{"SELECT 1;\nEND;", "END", 2},
		{"ROLLBACK;", "ROLLBACK", 1},
		{"ROLLBACK TO SAVEPOINT s;", "ROLLBACK", 1},
		{"Abort;", "ABORT", 1},
		{"SAVEPOINT s;", "SAVEPOINT", 1},
		{"RELEASE SAVEPOINT s;", "RELEASE", 1},
		{"release s;", "RELEASE", 1},
		{"PREPARE TRANSACTION 'x';", "PREPARE TRANSACTION", 1},
		{"COMMIT PREPARED 'x';", "COMMIT", 1},
		{"SELECT 1; commit", "COMMIT", 1},
		{"SELECT 1;;\n\n  COMMIT", "COMMIT", 3},
		{"-- leading comment\n/* block */ Commit;", "COMMIT", 2},
		// A literal ends where PostgreSQL ends it, so the COMMIT is visible.
		{"SELECT 'it''s'; COMMIT;", "COMMIT", 1},
		{"SELECT E'\\''; COMMIT;", "COMMIT", 1},
		{"SELECT 'a\\'; COMMIT;", "COMMIT", 1},
		{"SELECT $$x$$; COMMIT;", "COMMIT", 1},
		{"CREATE FUNCTION f() RETURNS int LANGUAGE sql BEGIN ATOMIC SELECT 1; END;\nCOMMIT;", "COMMIT", 2},
		{"CREATE RULE r AS ON INSERT TO t DO ALSO (INSERT INTO a VALUES (1); INSERT INTO b VALUES (2));\nROLLBACK;", "ROLLBACK", 2},
	} {
		statement, line, err := findTransactionControl(tc.sql)
		if err != nil || statement != tc.statement || line != tc.line {
			t.Errorf("%q: statement=%q line=%d err=%v", tc.sql, statement, line, err)
		}
	}
}

func TestFindTransactionControlIgnoresNonStatementText(t *testing.T) {
	for name, sql := range map[string]string{
		"line comments":        "-- COMMIT;\n-- BEGIN; ROLLBACK;\nSELECT 1;",
		"nested block comment": "/* BEGIN; /* COMMIT; */ ROLLBACK; */ SELECT 1;",
		"string literals":      "INSERT INTO t VALUES ('COMMIT; BEGIN; ROLLBACK;', 'it''s; END;');",
		"escape string":        "SELECT E'it\\'s; COMMIT;';",
		"unicode string":       "SELECT U&'COMMIT;', U&\"rollback\";",
		"bit strings":          "SELECT B'1010', X'FF', N'commit;';",
		"dollar body":          "CREATE FUNCTION f() RETURNS void LANGUAGE plpgsql AS $$ BEGIN COMMIT; ROLLBACK; END; $$;",
		"tagged dollar body":   "DO $body$ BEGIN PERFORM $$ COMMIT; $$; END $body$;",
		"quoted identifiers":   "CREATE TABLE \"commit\"(\"begin\" int, \"END\" int);",
		"identifiers":          "CREATE TABLE commit_log(begin_at timestamptz, rollback_reason text);\nALTER TABLE commit_log RENAME COLUMN begin_at TO started_at;",
		"dollar identifier":    "SELECT 1 AS a$commit$;",
		"positional parameter": "PREPARE q(int) AS SELECT $1; EXECUTE q(1); DEALLOCATE q;",
		"keyword not leading":  "SELECT 1 AS begin, 2 AS commit; ALTER TABLE t ADD COLUMN \"end\" int;",
		"atomic body":          "CREATE OR REPLACE FUNCTION f(x int) RETURNS int LANGUAGE sql\nBEGIN ATOMIC\n  SELECT CASE WHEN x > 0 THEN 1 ELSE 0 END;\n  SELECT 2;\nEND;\nSELECT 3;",
		"atomic procedure":     "CREATE PROCEDURE p() LANGUAGE sql BEGIN ATOMIC INSERT INTO t VALUES (1); END;",
		"rule action list":     "CREATE RULE r AS ON INSERT TO t DO ALSO (INSERT INTO a VALUES (1); INSERT INTO b VALUES (2));",
		"case end":             "SELECT CASE WHEN true THEN 1 END;",
		"empty":                "  \n-- nothing\n",
	} {
		t.Run(name, func(t *testing.T) {
			if statement, line, err := findTransactionControl(sql); err != nil || statement != "" {
				t.Fatalf("false rejection: statement=%q line=%d err=%v", statement, line, err)
			}
		})
	}
}

func TestFindTransactionControlRejectsUnterminatedInput(t *testing.T) {
	for _, sql := range []string{
		"SELECT 'x",
		"SELECT E'x\\'",
		"SELECT \"x",
		"SELECT 1; /* x",
		"SELECT $$ COMMIT;",
		"SELECT $tag$ x $$",
		"CREATE FUNCTION f() RETURNS int LANGUAGE sql BEGIN ATOMIC SELECT 1; COMMIT;",
		"SELECT (1; COMMIT;",
	} {
		if statement, _, err := findTransactionControl(sql); err == nil {
			t.Errorf("%q accepted (statement=%q)", sql, statement)
		}
	}
}

func TestRepositoryMigrationsHaveNoTransactionControl(t *testing.T) {
	paths, err := filepath.Glob("../../../migrations/*.sql")
	if err != nil || len(paths) == 0 {
		t.Fatal("no repository migrations", err)
	}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if statement, line, err := findTransactionControl(string(body)); err != nil || statement != "" {
			t.Errorf("%s: statement=%q line=%d err=%v", path, statement, line, err)
		}
	}
}

func TestDiscoverMigrationsRejectsTransactionControl(t *testing.T) {
	dir := writeMigrationFiles(t, map[string]string{
		"001_ok.up.sql":  "CREATE TABLE ok(id int);",
		"002_bad.up.sql": "CREATE TABLE bad(id int);\n  commit;",
	})
	_, err := discoverMigrations(dir)
	if err == nil || !strings.Contains(err.Error(), "migration 002_bad: line 2: COMMIT is transaction control") {
		t.Fatalf("err=%v", err)
	}
}
