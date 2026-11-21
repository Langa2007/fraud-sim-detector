package stepup

import (
	"strings"
	"testing"
	"time"
)

func TestStepupOrchestration(t *testing.T) {
	orch := NewOrchestrator()

	// 1. Swahili prompt formatting
	promptSw := FormatPrompt(ChannelUSSD, "sw", 50000, "Jane Doe")
	if !strings.Contains(promptSw, "Uthibitisho wa Kinga") || !strings.Contains(promptSw, "50000") {
		t.Errorf("Unexpected Swahili prompt: %s", promptSw)
	}

	// 2. Initiate challenge
	ch, err := orch.Initiate("tenantA", "evt_123", ChannelUSSD, "sw", 75000, "John Kamau", 1*time.Second)
	if err != nil {
		t.Fatalf("Initiate failed: %v", err)
	}
	if ch.Status != StatusPending || ch.ChallengeID == "" {
		t.Errorf("Unexpected challenge state: %+v", ch)
	}

	// 3. Retrieve
	got, err := orch.Get("tenantA", ch.ChallengeID)
	if err != nil || got.ChallengeID != ch.ChallengeID {
		t.Fatalf("Get failed: %v", err)
	}

	// Cross-tenant isolation
	_, err = orch.Get("tenantB", ch.ChallengeID)
	if err != ErrNotFound {
		t.Errorf("Expected ErrNotFound for wrong tenant, got %v", err)
	}

	// 4. Respond Approve
	approved, err := orch.Respond("tenantA", ch.ChallengeID, true)
	if err != nil {
		t.Fatalf("Respond failed: %v", err)
	}
	if approved.Status != StatusApproved {
		t.Errorf("Expected approved status, got %s", approved.Status)
	}

	// 5. Duplicate response rejection
	_, err = orch.Respond("tenantA", ch.ChallengeID, false)
	if err != ErrAlreadyDecided {
		t.Errorf("Expected ErrAlreadyDecided, got %v", err)
	}

	// 6. Test Escalation
	ch2, err := orch.Initiate("tenantA", "evt_456", ChannelUSSD, "en", 100000, "Mule Inc", 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	escalated, err := orch.Escalate("tenantA", ch2.ChallengeID, ChannelSMS)
	if err != nil {
		t.Fatalf("Escalate failed: %v", err)
	}
	if escalated.Channel != ChannelSMS || escalated.Status != StatusPending {
		t.Errorf("Unexpected escalated challenge: %+v", escalated)
	}

	oldCh, _ := orch.Get("tenantA", ch2.ChallengeID)
	if oldCh.Status != StatusEscalated || oldCh.EscalatedTo != ChannelSMS {
		t.Errorf("Expected old challenge to be escalated, got %+v", oldCh)
	}
}
