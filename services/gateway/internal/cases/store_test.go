package cases

import (
	"path/filepath"
	"testing"
	"time"

	"kinga/services/gateway/internal/engine"
)

func TestCasesStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cases.jsonl")

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	// 1. Open a case
	result := engine.Result{
		Decision:      "HOLD",
		Mode:          "shadow",
		RiskScore:     0.92,
		RiskBand:      "critical",
		TenantEventID: "evt_case_1",
		EvidenceID:    "ev_1_abcdef",
		PolicyVersion: "2026-10-28.3",
		Reasons: []engine.Reason{
			{Code: "SIM_SWAP_RECENT", Detail: "SIM swap 20m ago", Weight: 0.5},
		},
	}

	c, err := store.OpenCase("tenantA", result)
	if err != nil {
		t.Fatalf("OpenCase failed: %v", err)
	}
	if c.CaseID == "" || c.Status != "open" || c.Tenant != "tenantA" {
		t.Errorf("Unexpected created case: %+v", c)
	}

	// 2. List cases
	list := store.List("tenantA", "open", 10)
	if len(list) != 1 || list[0].CaseID != c.CaseID {
		t.Errorf("Expected 1 open case, got %d", len(list))
	}

	// Cross-tenant list isolation
	otherList := store.List("tenantB", "open", 10)
	if len(otherList) != 0 {
		t.Errorf("Expected 0 cases for tenantB, got %d", len(otherList))
	}

	// 3. Assign case
	actionAssign := Action{
		ActionID:  "act_1",
		Type:      "assign",
		Reviewer:  "supervisor",
		Assignee:  "analyst-42",
		CreatedAt: time.Now().UTC(),
	}
	updated, err := store.Apply("tenantA", c.CaseID, actionAssign)
	if err != nil {
		t.Fatalf("Assign action failed: %v", err)
	}
	if updated.Assignee != "analyst-42" || updated.Status != "assigned" {
		t.Errorf("Unexpected updated case after assign: %+v", updated)
	}

	// 4. Add Note
	actionNote := Action{
		ActionID:  "act_2",
		Type:      "note",
		Reviewer:  "analyst-42",
		Note:      "Investigating SIM swap with Safaricom verification",
		CreatedAt: time.Now().UTC(),
	}
	updated, err = store.Apply("tenantA", c.CaseID, actionNote)
	if err != nil {
		t.Fatalf("Note action failed: %v", err)
	}
	if len(updated.History) != 2 {
		t.Errorf("Expected 2 history items, got %d", len(updated.History))
	}

	// 5. Release case with reason
	actionRelease := Action{
		ActionID:  "act_3",
		Type:      "release",
		Reviewer:  "analyst-42",
		Note:      "Customer confirmed routine device upgrade via out-of-band call",
		CreatedAt: time.Now().UTC(),
	}
	updated, err = store.Apply("tenantA", c.CaseID, actionRelease)
	if err != nil {
		t.Fatalf("Release action failed: %v", err)
	}
	if updated.Status != "released" {
		t.Errorf("Expected status released, got %s", updated.Status)
	}

	// 6. Persistence & tamper-evident reload
	store.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Reopen failed: %v", err)
	}
	defer reopened.Close()

	persisted, err := reopened.Get("tenantA", c.CaseID)
	if err != nil || persisted.Status != "released" || len(persisted.History) != 3 {
		t.Errorf("Persisted case mismatch: %+v", persisted)
	}
}
