package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNeonRehearsalProviderCreatesAndDeletesCredentialSafeBranch(t *testing.T) {
	var created, deleted bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer api-secret" {
			t.Error("missing authorization")
		}
		switch request.Method {
		case http.MethodPost:
			created = true
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"branch":{"id":"br-test-123","name":"queryshift-test","created_at":"2026-10-21T00:00:00Z"},"connection_uris":[{"connection_uri":"postgresql://user:secret@branch.example/app"}]}`))
		case http.MethodDelete:
			deleted = true
			writer.WriteHeader(http.StatusOK)
		default:
			writer.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	contract, err := BuildChangeContract("CREATE TABLE public.events(id bigint)", contractWorkspace())
	if err != nil {
		t.Fatal(err)
	}
	provider := NeonRehearsalProvider{ProjectID: "project-test", ParentID: "br-parent-123", APIKey: "api-secret", APIBase: server.URL, Client: server.Client()}
	target, err := provider.Create(context.Background(), contract)
	if err != nil {
		t.Fatal(err)
	}
	if !created || target.ID != "br-test-123" || !strings.Contains(target.ConnectionString, "secret") {
		t.Fatalf("unexpected target: %+v", target)
	}
	encoded, _ := json.Marshal(target)
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "postgresql") {
		t.Fatalf("target serialized credentials: %s", encoded)
	}
	if err := provider.Delete(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("branch was not deleted")
	}
	if !target.CreatedAt.Equal(time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("created_at=%s", target.CreatedAt)
	}
}
