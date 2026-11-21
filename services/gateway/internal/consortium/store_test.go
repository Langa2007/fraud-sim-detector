package consortium

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"
	"time"
)

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func TestConsortiumStore_OpenValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.jsonl")

	_, err := Open(path, 1)
	if err == nil {
		t.Fatal("expected error when opening with minimumIndependent < 2")
	}

	store, err := Open(path, 2)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()
}

func TestConsortiumStore_ContributeAndPrivacyFloor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.jsonl")

	store, err := Open(path, 2)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	token := hashToken("+254712345678")

	// 1. Invalid contributions
	if _, err := store.Contribute("", "msisdn", token, 0.8, 24*time.Hour); err == nil {
		t.Error("expected error on empty tenant")
	}
	if _, err := store.Contribute("tenant1", "invalid_ns", token, 0.8, 24*time.Hour); err == nil {
		t.Error("expected error on invalid namespace")
	}
	if _, err := store.Contribute("tenant1", "msisdn", "not-a-token", 0.8, 24*time.Hour); err == nil {
		t.Error("expected error on invalid token hex")
	}
	if _, err := store.Contribute("tenant1", "msisdn", token, 1.5, 24*time.Hour); err == nil {
		t.Error("expected error on confidence > 1")
	}

	// 2. First contribution from tenant1
	r1, err := store.Contribute("tenant1", "msisdn", token, 0.9, 24*time.Hour)
	if err != nil {
		t.Fatalf("tenant1 contribution failed: %v", err)
	}
	if r1.Sequence != 1 {
		t.Errorf("expected sequence 1, got %d", r1.Sequence)
	}

	// Duplicate contribution by tenant1 should fail
	if _, err := store.Contribute("tenant1", "msisdn", token, 0.9, 24*time.Hour); err == nil {
		t.Error("expected error on duplicate contribution by tenant1")
	}

	// 3. Privacy floor check: query before 2 independent confirmations must return false
	now := time.Now().UTC()
	_, found := store.Query("msisdn", token, now)
	if found {
		t.Error("expected indicator to be hidden below privacy floor (only 1 confirmation)")
	}

	// 4. Second contribution from tenant2
	r2, err := store.Contribute("tenant2", "msisdn", token, 0.7, 24*time.Hour)
	if err != nil {
		t.Fatalf("tenant2 contribution failed: %v", err)
	}
	if r2.Sequence != 2 {
		t.Errorf("expected sequence 2, got %d", r2.Sequence)
	}
	if r2.PreviousHash != r1.Hash {
		t.Errorf("expected hash chain: PreviousHash %s != %s", r2.PreviousHash, r1.Hash)
	}

	// 5. Query now meets privacy floor >= 2
	res, found := store.Query("msisdn", token, now)
	if !found {
		t.Fatal("expected indicator to be found with 2 independent confirmations")
	}
	if res.IndependentConfirmations != 2 {
		t.Errorf("expected 2 confirmations, got %d", res.IndependentConfirmations)
	}
	// Confidence should be avg of 0.9 and 0.7 ~ 0.8
	if res.Confidence < 0.79 || res.Confidence > 0.81 {
		t.Errorf("expected confidence ~0.8, got %f", res.Confidence)
	}

	// Check member contribution counts
	if store.MemberContributionCount("tenant1") != 1 {
		t.Errorf("expected tenant1 count 1, got %d", store.MemberContributionCount("tenant1"))
	}
	if store.MemberContributionCount("tenant2") != 1 {
		t.Errorf("expected tenant2 count 1, got %d", store.MemberContributionCount("tenant2"))
	}
	if store.MemberContributionCount("tenant3") != 0 {
		t.Errorf("expected tenant3 count 0, got %d", store.MemberContributionCount("tenant3"))
	}
}

func TestConsortiumStore_DisputeAndResolve(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.jsonl")

	store, err := Open(path, 2)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	token := hashToken("ACC-998877")
	_, _ = store.Contribute("bank_a", "account", token, 0.85, 48*time.Hour)
	_, _ = store.Contribute("bank_b", "account", token, 0.95, 48*time.Hour)

	now := time.Now().UTC()
	_, found := store.Query("account", token, now)
	if !found {
		t.Fatal("indicator should be active and found")
	}

	// Non-contributor cannot dispute
	if _, err := store.Dispute("bank_c", "account", token, "False positive report by bank c"); err == nil {
		t.Error("expected error when non-contributor disputes")
	}

	// Contributor disputes
	disputeRec, err := store.Dispute("bank_a", "account", token, "Customer provided valid KYC clarification")
	if err != nil {
		t.Fatalf("dispute failed: %v", err)
	}
	if disputeRec.Indicator.Status != "disputed" {
		t.Errorf("expected status disputed, got %s", disputeRec.Indicator.Status)
	}

	// Query for disputed indicator should return false
	if _, found := store.Query("account", token, now); found {
		t.Error("disputed indicator should not be returned in query")
	}

	// Governance resolution: remove
	resRec, err := store.Resolve("account", token, "remove", "Governance committee confirmed false positive")
	if err != nil {
		t.Fatalf("resolve remove failed: %v", err)
	}
	if resRec.Indicator.Status != "removed" {
		t.Errorf("expected status removed, got %s", resRec.Indicator.Status)
	}

	// Governance resolution: reinstate
	reinstateRec, err := store.Resolve("account", token, "reinstate", "Governance committee re-confirmed fraud pattern")
	if err != nil {
		t.Fatalf("resolve reinstate failed: %v", err)
	}
	if reinstateRec.Indicator.Status != "active" {
		t.Errorf("expected status active, got %s", reinstateRec.Indicator.Status)
	}

	// Should be queryable again
	if _, found := store.Query("account", token, now); !found {
		t.Error("reinstated indicator should be queryable again")
	}
}

func TestConsortiumStore_PersistenceAndReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.jsonl")

	store1, err := Open(path, 2)
	if err != nil {
		t.Fatalf("failed to open store1: %v", err)
	}

	token := hashToken("device_fingerprint_xyz")
	_, err = store1.Contribute("telco_1", "device", token, 0.90, 72*time.Hour)
	if err != nil {
		t.Fatalf("contribute 1: %v", err)
	}
	_, err = store1.Contribute("telco_2", "device", token, 0.80, 72*time.Hour)
	if err != nil {
		t.Fatalf("contribute 2: %v", err)
	}
	store1.Close()

	// Reopen ledger and verify integrity verification
	store2, err := Open(path, 2)
	if err != nil {
		t.Fatalf("failed to reopen store: %v", err)
	}
	defer store2.Close()

	res, found := store2.Query("device", token, time.Now().UTC())
	if !found {
		t.Fatal("expected indicator to be recovered after reopen")
	}
	if res.IndependentConfirmations != 2 {
		t.Errorf("expected 2 confirmations, got %d", res.IndependentConfirmations)
	}
	if store2.MemberContributionCount("telco_1") != 1 || store2.MemberContributionCount("telco_2") != 1 {
		t.Error("member counts not preserved across reopen")
	}
}
