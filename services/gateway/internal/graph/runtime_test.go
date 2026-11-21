package graph

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func TestGraphRuntime_LoadAndLookup(t *testing.T) {
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "graph_features.jsonl")

	token1 := hashToken("mule_wallet_1")
	token2 := hashToken("normal_account_2")

	content := fmt.Sprintf(`{"namespace":"wallet","token":"%s","window_days":30,"degree":12,"inbound_transfers":25,"outbound_transfers":24,"independent_tenants":4,"mule_likelihood":0.89,"community_token":"comm123","version":"kinga.graph.v1"}
{"namespace":"account","token":"%s","window_days":30,"degree":2,"inbound_transfers":2,"outbound_transfers":1,"independent_tenants":1,"mule_likelihood":0.05,"community_token":"comm456","version":"kinga.graph.v1"}
`, token1, token2)

	if err := os.WriteFile(snapshotPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write snapshot file: %v", err)
	}

	rt, err := New(snapshotPath)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}

	// 1. Lookup token1 with minIndependent = 2 -> Should match (independent_tenants = 4)
	feat1, ok := rt.Lookup("wallet", token1, 2)
	if !ok {
		t.Fatal("expected token1 to be found")
	}
	if feat1.MuleLikelihood != 0.89 || feat1.Degree != 12 {
		t.Errorf("unexpected feat1 values: %+v", feat1)
	}

	// 2. Lookup token1 with minIndependent = 5 -> Should return false (independent_tenants = 4 < 5)
	if _, ok := rt.Lookup("wallet", token1, 5); ok {
		t.Error("expected token1 to be filtered out when minIndependent=5")
	}

	// 3. Lookup token2 with minIndependent = 2 -> Should return false (independent_tenants = 1 < 2)
	if _, ok := rt.Lookup("account", token2, 2); ok {
		t.Error("expected token2 to be filtered out below minIndependent=2")
	}

	// 4. Lookup token2 with minIndependent = 1 -> Should match
	feat2, ok := rt.Lookup("account", token2, 1)
	if !ok {
		t.Fatal("expected token2 to be found with minIndependent=1")
	}
	if feat2.MuleLikelihood != 0.05 {
		t.Errorf("expected 0.05 mule likelihood, got %f", feat2.MuleLikelihood)
	}

	// 5. Lookup unknown token
	if _, ok := rt.Lookup("wallet", hashToken("unknown"), 1); ok {
		t.Error("expected unknown token to return false")
	}

	// 6. Invalid namespace or token pattern
	if _, ok := rt.Lookup("invalid_namespace", token1, 1); ok {
		t.Error("expected invalid namespace to return false")
	}
	if _, ok := rt.Lookup("wallet", "short-token", 1); ok {
		t.Error("expected invalid token pattern to return false")
	}
}

func TestGraphRuntime_Validation(t *testing.T) {
	dir := t.TempDir()

	// Corrupt JSON
	bad1 := filepath.Join(dir, "bad1.jsonl")
	_ = os.WriteFile(bad1, []byte(`not json`), 0600)
	rt, _ := New("")
	if err := rt.Reload(bad1); err == nil {
		t.Error("expected reload error on invalid json")
	}

	// Invalid version
	token := hashToken("tok1")
	bad2 := filepath.Join(dir, "bad2.jsonl")
	_ = os.WriteFile(bad2, []byte(fmt.Sprintf(`{"namespace":"wallet","token":"%s","window_days":30,"degree":1,"inbound_transfers":1,"outbound_transfers":1,"independent_tenants":2,"mule_likelihood":0.5,"community_token":"","version":"v2"}`, token)), 0600)
	if err := rt.Reload(bad2); err == nil {
		t.Error("expected reload error on bad version")
	}

	// Duplicate key
	bad3 := filepath.Join(dir, "bad3.jsonl")
	dupContent := fmt.Sprintf(`{"namespace":"wallet","token":"%s","window_days":30,"degree":1,"inbound_transfers":1,"outbound_transfers":1,"independent_tenants":2,"mule_likelihood":0.5,"community_token":"","version":"kinga.graph.v1"}
{"namespace":"wallet","token":"%s","window_days":30,"degree":2,"inbound_transfers":2,"outbound_transfers":2,"independent_tenants":2,"mule_likelihood":0.6,"community_token":"","version":"kinga.graph.v1"}`, token, token)
	_ = os.WriteFile(bad3, []byte(dupContent), 0600)
	if err := rt.Reload(bad3); err == nil {
		t.Error("expected reload error on duplicate key")
	}
}
