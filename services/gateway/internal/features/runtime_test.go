package features

import (
	"testing"
	"time"

	"kinga/services/gateway/internal/engine"
)

func TestFeatureRuntime(t *testing.T) {
	rt := NewRuntime()
	now := time.Now()

	ev1 := &engine.Event{
		EventType:  "funds_transfer",
		OccurredAt: now,
	}
	ev1.Subject.AccountRef = "acc_100"
	ev1.Subject.MSISDN = "+254712345678"
	ev1.Device.DeviceID = "dvc_1"
	ev1.Transaction.Amount = 1000
	ev1.Transaction.Beneficiary.Ref = "ben_A"

	rt.Observe("tenantA", ev1)

	// First time device and beneficiary should be first_seen=true
	if !ev1.Signals.DeviceFirstSeen {
		t.Errorf("Expected DeviceFirstSeen=true on first event")
	}
	if !ev1.Signals.BeneficiaryFirstSeen {
		t.Errorf("Expected BeneficiaryFirstSeen=true on first event")
	}
	if ev1.Signals.TransfersInWindow != 1 || ev1.Signals.NewBeneficiariesInWindow != 1 {
		t.Errorf("Expected 1 transfer and 1 new ben, got transfers=%d, bens=%d",
			ev1.Signals.TransfersInWindow, ev1.Signals.NewBeneficiariesInWindow)
	}

	// Second event with same device, same beneficiary 2 minutes later
	ev2 := &engine.Event{
		EventType:  "funds_transfer",
		OccurredAt: now.Add(2 * time.Minute),
	}
	ev2.Subject.AccountRef = "acc_100"
	ev2.Device.DeviceID = "dvc_1"
	ev2.Transaction.Amount = 2000
	ev2.Transaction.Beneficiary.Ref = "ben_A"

	rt.Observe("tenantA", ev2)

	if ev2.Signals.DeviceFirstSeen {
		t.Errorf("Expected DeviceFirstSeen=false on second event with same device")
	}
	if ev2.Signals.BeneficiaryFirstSeen {
		t.Errorf("Expected BeneficiaryFirstSeen=false on second event with same beneficiary")
	}
	if ev2.Signals.TransfersInWindow != 2 || ev2.Signals.NewBeneficiariesInWindow != 1 {
		t.Errorf("Expected 2 transfers and 1 unique ben, got transfers=%d, bens=%d",
			ev2.Signals.TransfersInWindow, ev2.Signals.NewBeneficiariesInWindow)
	}

	// Third event with new beneficiary
	ev3 := &engine.Event{
		EventType:  "funds_transfer",
		OccurredAt: now.Add(5 * time.Minute),
	}
	ev3.Subject.AccountRef = "acc_100"
	ev3.Device.DeviceID = "dvc_1"
	ev3.Transaction.Amount = 5000
	ev3.Transaction.Beneficiary.Ref = "ben_B"

	rt.Observe("tenantA", ev3)

	if !ev3.Signals.BeneficiaryFirstSeen {
		t.Errorf("Expected BeneficiaryFirstSeen=true for new beneficiary ben_B")
	}
	if ev3.Signals.TransfersInWindow != 3 || ev3.Signals.NewBeneficiariesInWindow != 2 {
		t.Errorf("Expected 3 transfers and 2 unique bens, got transfers=%d, bens=%d",
			ev3.Signals.TransfersInWindow, ev3.Signals.NewBeneficiariesInWindow)
	}
}
