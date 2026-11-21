package outcomes

import (
	"path/filepath"
	"testing"
	"time"
)

func TestOutcomesStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "outcomes.jsonl")

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	loss := 45000.0
	sub := Submission{
		SubmissionID:  "sub_1",
		TenantEventID: "evt_fraud_1",
		EvidenceID:    "ev_100_abc",
		Label:         "confirmed_fraud",
		Reviewer:      "analyst-99",
		Notes:         "Confirmed SIM-swap takeover loss",
		LossKES:       &loss,
		ObservedAt:    time.Now().UTC(),
	}

	// Append outcome
	rec, err := store.Append("tenantA", sub)
	if err != nil {
		t.Fatalf("Append failed: %v", err)
	}
	if rec.Sequence != 1 || rec.Hash == "" || rec.Tenant != "tenantA" {
		t.Errorf("Unexpected record: %+v", rec)
	}

	// Idempotent duplicate replay
	recDup, err := store.Append("tenantA", sub)
	if err != nil {
		t.Fatalf("Idempotent replay failed: %v", err)
	}
	if recDup.Hash != rec.Hash {
		t.Errorf("Expected identical hash on idempotent submission replay")
	}

	// Idempotency conflict with different label
	subConflict := sub
	subConflict.Label = "false_positive"
	_, err = store.Append("tenantA", subConflict)
	if err == nil {
		t.Errorf("Expected idempotency conflict error, got nil")
	}

	// Second distinct submission
	sub2 := Submission{
		SubmissionID:  "sub_2",
		TenantEventID: "evt_clean_2",
		EvidenceID:    "ev_101_def",
		Label:         "false_positive",
		Reviewer:      "analyst-99",
		Notes:         "Verified legitimate customer transfer",
		ObservedAt:    time.Now().UTC(),
	}
	rec2, err := store.Append("tenantA", sub2)
	if err != nil {
		t.Fatalf("Append sub2 failed: %v", err)
	}
	if rec2.Sequence != 2 || rec2.PreviousHash != rec.Hash {
		t.Errorf("Hash chain broken: rec2.PreviousHash=%s, rec.Hash=%s", rec2.PreviousHash, rec.Hash)
	}

	// Reload & verify chain integrity
	store.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Reopen failed: %v", err)
	}
	defer reopened.Close()
}
