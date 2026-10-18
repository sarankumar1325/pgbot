package app

import (
	"strings"
	"testing"
)

func TestStripUnsupportedConnectionParams(t *testing.T) {
	got := stripUnsupportedConnectionParams("postgresql://owner:secret@example.test/db?sslmode=require&channel_binding=require")
	if strings.Contains(got, "channel_binding") {
		t.Fatalf("channel_binding was not removed: %s", got)
	}
	if !strings.Contains(got, "sslmode=require") {
		t.Fatalf("sslmode was lost: %s", got)
	}

	got = stripUnsupportedConnectionParams("host=example.test password=secret channel_binding=require sslmode=require")
	if strings.Contains(got, "channel_binding") || !strings.Contains(got, "password=secret") {
		t.Fatalf("keyword connection string was changed incorrectly: %s", got)
	}
}

func TestWithEnvironmentReplacesCredential(t *testing.T) {
	got := withEnvironment([]string{"PATH=/bin", "DATABASE_URL=old-secret"}, "DATABASE_URL", "new-secret")
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "old-secret") || strings.Count(joined, "DATABASE_URL=") != 1 {
		t.Fatalf("environment did not replace the credential: %v", got)
	}
}
