package app

import (
	"errors"
	"testing"

	"github.com/sarankumar1325/queryshift/internal/erd"
)

func TestRehearsalVerificationRequiresManualBusinessInvariant(t *testing.T) {
	contract, err := BuildChangeContract("UPDATE public.accounts SET id = id WHERE id = 1", contractWorkspace())
	if err != nil {
		t.Fatal(err)
	}
	result := &ChangeResult{Command: "UPDATE 1", RowsAffected: 1, Duration: 10, SnapshotAfter: true}
	results := verifyRehearsal(contract, result, contractWorkspace(), nil)
	if rehearsalStatus(results) != "needs_review" {
		t.Fatalf("DML rehearsal must require a domain assertion: %+v", results)
	}
}

func TestRehearsalVerificationChecksIndexPresence(t *testing.T) {
	contract, err := BuildChangeContract("CREATE INDEX accounts_id_idx ON public.accounts(id)", contractWorkspace())
	if err != nil {
		t.Fatal(err)
	}
	result := &ChangeResult{Command: "CREATE INDEX", SnapshotAfter: true, BeforeSchemaSHA256: "aaa", AfterSchemaSHA256: "bbb"}
	after := contractWorkspace()
	after.Schema.Tables[0].Indexes = []erd.Index{{Name: "accounts_id_idx", Def: "btree (id)"}}
	results := verifyRehearsal(contract, result, after, nil)
	if rehearsalStatus(results) != "verified" {
		t.Fatalf("present valid index should verify: %+v", results)
	}
	results = verifyRehearsal(contract, result, after, errors.New("snapshot failed"))
	if rehearsalStatus(results) != "failed" {
		t.Fatalf("missing post-change evidence should fail: %+v", results)
	}
}
