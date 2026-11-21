package mule

import (
	"testing"
	"time"

	"kinga/services/gateway/internal/engine"
)

func TestMuleDetector_RapidFloatDepletion(t *testing.T) {
	d := New()

	drain := 0.85
	ev := &engine.Event{
		EventType:  "agent_float_movement",
		OccurredAt: time.Now().UTC(),
		Channel:    "agent",
	}
	ev.Subject.AccountRef = "AGENT-001"
	ev.Signals.BalanceDrainRatio = &drain
	ev.Signals.WindowMinutes = 20

	anomaly := d.Evaluate("bank_a", ev)
	if !anomaly.IsAnomaly {
		t.Fatal("expected rapid float depletion to trigger anomaly")
	}
	if anomaly.Code != "AGENT_FLOAT_DEPLETION" {
		t.Errorf("expected code AGENT_FLOAT_DEPLETION, got %s", anomaly.Code)
	}

	// Normal movement below 80% should not trigger
	lowDrain := 0.40
	evNormal := &engine.Event{
		EventType:  "agent_float_movement",
		OccurredAt: time.Now().UTC(),
		Channel:    "agent",
	}
	evNormal.Subject.AccountRef = "AGENT-002"
	evNormal.Signals.BalanceDrainRatio = &lowDrain
	evNormal.Signals.WindowMinutes = 20

	if a := d.Evaluate("bank_a", evNormal); a.IsAnomaly {
		t.Errorf("normal float movement should not trigger anomaly: %+v", a)
	}
}

func TestMuleDetector_Structuring(t *testing.T) {
	d := New()
	now := time.Now().UTC()

	ev1 := &engine.Event{
		EventType:  "funds_transfer",
		OccurredAt: now,
		Channel:    "mobile_app",
	}
	ev1.Subject.AccountRef = "ACC-MULE-1"
	ev1.Transaction.Amount = 98500
	ev1.Transaction.Currency = "KES"

	// First transaction in structuring band
	a1 := d.Evaluate("bank_a", ev1)
	if a1.IsAnomaly {
		t.Errorf("single non-agent transaction should not immediately trigger structuring: %+v", a1)
	}

	// Second transaction within 1h in structuring band
	ev2 := &engine.Event{
		EventType:  "funds_transfer",
		OccurredAt: now.Add(5 * time.Minute),
		Channel:    "mobile_app",
	}
	ev2.Subject.AccountRef = "ACC-MULE-1"
	ev2.Transaction.Amount = 99000
	ev2.Transaction.Currency = "KES"

	a2 := d.Evaluate("bank_a", ev2)
	if !a2.IsAnomaly {
		t.Fatal("repeated transfer just below 100k KES should trigger structuring anomaly")
	}
	if a2.Code != "STRUCTURING_DETECTED" {
		t.Errorf("expected STRUCTURING_DETECTED, got %s", a2.Code)
	}
}

func TestMuleDetector_BurstCashOut(t *testing.T) {
	d := New()
	ev := &engine.Event{
		EventType:  "funds_transfer",
		OccurredAt: time.Now().UTC(),
		Channel:    "agent",
	}
	ev.Subject.AccountRef = "AGENT-BURST-1"
	ev.Signals.TransfersInWindow = 7
	ev.Signals.RapidOutbound = true

	anomaly := d.Evaluate("bank_a", ev)
	if !anomaly.IsAnomaly {
		t.Fatal("expected burst cash-out to trigger anomaly")
	}
	if anomaly.Code != "AGENT_BURST_CASH_OUT" {
		t.Errorf("expected AGENT_BURST_CASH_OUT, got %s", anomaly.Code)
	}
}
