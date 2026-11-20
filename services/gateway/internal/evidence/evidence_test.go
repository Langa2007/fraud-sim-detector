package evidence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEvidenceHashChain(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "evidence.jsonl")

	l, err := Open(logPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer l.Close()

	eid1, err := l.Append("tenantA", "evt_1", []byte("hash1"), []byte(`{"feat":1}`), func(eid string) ([]byte, error) {
		return json.Marshal(map[string]any{"decision": "ALLOW", "evidence_id": eid})
	})
	if err != nil {
		t.Fatalf("Append eid1 failed: %v", err)
	}
	if eid1 == "" {
		t.Errorf("Expected non-empty eid1")
	}

	eid2, err := l.Append("tenantA", "evt_2", []byte("hash2"), []byte(`{"feat":2}`), func(eid string) ([]byte, error) {
		return json.Marshal(map[string]any{"decision": "HOLD", "evidence_id": eid})
	})
	if err != nil {
		t.Fatalf("Append eid2 failed: %v", err)
	}
	if eid2 == "" {
		t.Errorf("Expected non-empty eid2")
	}

	count, head, err := l.Verify()
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if count != 2 || head == "" {
		t.Errorf("Expected count 2, head non-empty; got count %d, head %s", count, head)
	}

	// Reopen and verify it loads without error
	l.Close()
	l2, err := Open(logPath)
	if err != nil {
		t.Fatalf("Re-open failed: %v", err)
	}
	l2.Close()

	// Tamper test
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	// Tamper one byte in the file
	tampered := append([]byte(nil), raw...)
	tampered[len(tampered)-10] = 'X'
	if err := os.WriteFile(logPath, tampered, 0600); err != nil {
		t.Fatal(err)
	}

	_, err = Open(logPath)
	if err == nil {
		t.Fatalf("Expected Open to fail on tampered evidence file, but it succeeded")
	}
}
