package app

import "testing"

func TestAnalyzeChangeClassifiesGuardedWrites(t *testing.T) {
	tests := []struct {
		sql, confirmation string
		destructive       bool
	}{
		{"INSERT INTO users(name) VALUES ('semi;colon')", "APPLY", false},
		{"UPDATE users SET active = false WHERE id = 1", "BACKUP AND APPLY", false},
		{"UPDATE users SET active = false", "BACKUP AND APPLY DESTRUCTIVE", true},
		{"UPDATE users SET note = 'WHERE id = 1'", "BACKUP AND APPLY DESTRUCTIVE", true},
		{"DELETE FROM users /* WHERE id = 1 */", "BACKUP AND APPLY DESTRUCTIVE", true},
		{"DELETE FROM users", "BACKUP AND APPLY DESTRUCTIVE", true},
		{"CREATE INDEX users_email_idx ON users(email)", "APPLY", false},
		{"ALTER TABLE users DROP COLUMN legacy", "BACKUP AND APPLY DESTRUCTIVE", true},
		{"DROP TABLE users", "BACKUP AND APPLY DESTRUCTIVE", true},
	}
	for _, test := range tests {
		plan, err := AnalyzeChange(test.sql)
		if err != nil {
			t.Fatalf("AnalyzeChange(%q): %v", test.sql, err)
		}
		if plan.Destructive != test.destructive || plan.RequiredConfirmation != test.confirmation {
			t.Fatalf("AnalyzeChange(%q) = destructive %v confirmation %q", test.sql, plan.Destructive, plan.RequiredConfirmation)
		}
	}
}

func TestCreateChangeIncludesGeneratedRollback(t *testing.T) {
	plan, err := AnalyzeChange(`CREATE UNIQUE INDEX users_email_idx ON public.users(email)`)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Versioned || plan.RecoveryLevel != "rollback generated" {
		t.Fatalf("unexpected recovery classification: %+v", plan)
	}
	if plan.RollbackSQL != "DROP INDEX IF EXISTS users_email_idx" {
		t.Fatalf("unexpected rollback SQL %q", plan.RollbackSQL)
	}
}

func TestAnalyzeChangeRejectsUnsafeScopeAndBatches(t *testing.T) {
	for _, sql := range []string{
		"ALTER SYSTEM SET fsync = off",
		"CREATE ROLE admin SUPERUSER",
		"INSERT INTO a VALUES (1); DELETE FROM b",
		"SELECT * FROM users",
		"CREATE INDEX CONCURRENTLY users_email_idx ON users(email)",
	} {
		if _, err := AnalyzeChange(sql); err == nil {
			t.Fatalf("AnalyzeChange(%q) should fail", sql)
		}
	}
}

func TestSingleStatementAllowsQuotedSemicolonsAndComments(t *testing.T) {
	for _, sql := range []string{
		"INSERT INTO notes(body) VALUES ('hello; world');",
		"UPDATE notes SET body = $$hello; world$$ WHERE id = 1",
		"/* ; */ DELETE FROM notes WHERE id = 1 -- ;\n",
	} {
		if err := validateSingleStatement(sql); err != nil {
			t.Fatalf("validateSingleStatement(%q): %v", sql, err)
		}
	}
}
