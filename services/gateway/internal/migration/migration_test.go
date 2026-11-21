package migration

import (
	"testing"
)

func TestMigrationManager(t *testing.T) {
	mgr := NewManager()

	// Default is shadow
	if mode := mgr.EvaluateMode("tenantA", "subj_1"); mode != ModeShadow {
		t.Errorf("Expected ModeShadow by default, got %s", mode)
	}

	// Set staged rollout: 25% enforce
	mgr.SetConfig("tenantA", Config{
		TargetMode:        ModeEnforce,
		RolloutPercentage: 25,
		FrictionCeiling:   0.02,
		FrictionWindow:    100,
	})

	shadowCount := 0
	enforceCount := 0
	for i := 0; i < 1000; i++ {
		mode := mgr.EvaluateMode("tenantA", string(rune(i)))
		if mode == ModeEnforce {
			enforceCount++
		} else {
			shadowCount++
		}
	}

	// 25% should be approximately 250 out of 1000
	if enforceCount < 200 || enforceCount > 300 {
		t.Errorf("Expected ~25%% enforce (200-300), got %d", enforceCount)
	}

	// Test Kill Switch
	mgr.TripKillSwitch("tenantA", "manual operator kill-switch")
	if mode := mgr.EvaluateMode("tenantA", "subj_always_shadow"); mode != ModeShadow {
		t.Errorf("Expected ModeShadow after kill switch tripped, got %s", mode)
	}

	// Reset config to 100% enforce with 2% friction ceiling
	mgr.SetConfig("tenantA", Config{
		TargetMode:        ModeEnforce,
		RolloutPercentage: 100,
		FrictionCeiling:   0.02,
		FrictionWindow:    100,
	})

	// Simulate high friction spike: 10 holds out of 60 events (16.6% friction > 2% ceiling)
	for i := 0; i < 50; i++ {
		mgr.RecordOutcome("tenantA", false)
	}
	for i := 0; i < 10; i++ {
		mgr.RecordOutcome("tenantA", true)
	}

	// Should have triggered automatic emergency rollback to shadow mode
	if mode := mgr.EvaluateMode("tenantA", "subj_1"); mode != ModeShadow {
		t.Errorf("Expected auto-rollback to ModeShadow after friction spike, got %s", mode)
	}
	reason := mgr.GetAutoRollbackReason("tenantA")
	if reason == "" {
		t.Errorf("Expected auto-rollback reason to be recorded")
	}
}
