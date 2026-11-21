package migration

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sync"
)

type Mode string

const (
	ModeShadow   Mode = "shadow"
	ModeAdvisory Mode = "advisory"
	ModeEnforce  Mode = "enforce"
)

type Config struct {
	TargetMode        Mode    `json:"target_mode"`
	RolloutPercentage int     `json:"rollout_percentage"` // 0, 5, 25, 100
	KillSwitchActive  bool    `json:"kill_switch_active"`
	FrictionCeiling   float64 `json:"friction_ceiling"` // default 0.015 (1.5%)
	FrictionWindow    int     `json:"friction_window"`  // number of events to monitor
}

type Manager struct {
	mu           sync.Mutex
	configs      map[string]*Config
	assessments  map[string]int
	frictions    map[string]int
	autoRollback map[string]string
}

func NewManager() *Manager {
	return &Manager{
		configs:      make(map[string]*Config),
		assessments:  make(map[string]int),
		frictions:    make(map[string]int),
		autoRollback: make(map[string]string),
	}
}

func (m *Manager) GetConfig(tenant string) Config {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg, ok := m.configs[tenant]
	if !ok {
		return Config{
			TargetMode:        ModeShadow,
			RolloutPercentage: 0,
			KillSwitchActive:  false,
			FrictionCeiling:   0.015,
			FrictionWindow:    500,
		}
	}
	return *cfg
}

func (m *Manager) SetConfig(tenant string, cfg Config) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cfg.RolloutPercentage < 0 {
		cfg.RolloutPercentage = 0
	} else if cfg.RolloutPercentage > 100 {
		cfg.RolloutPercentage = 100
	}
	if cfg.FrictionCeiling <= 0 {
		cfg.FrictionCeiling = 0.015
	}
	if cfg.FrictionWindow <= 0 {
		cfg.FrictionWindow = 500
	}

	m.configs[tenant] = &cfg
	delete(m.autoRollback, tenant)
}

func (m *Manager) TripKillSwitch(tenant, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg, ok := m.configs[tenant]
	if !ok {
		cfg = &Config{
			TargetMode:        ModeShadow,
			RolloutPercentage: 0,
			FrictionCeiling:   0.015,
			FrictionWindow:    500,
		}
		m.configs[tenant] = cfg
	}
	cfg.KillSwitchActive = true
	m.autoRollback[tenant] = reason
}

// EvaluateMode decides the operational mode for a given tenant event.
// Uses deterministic hash bucketing based on event ID or account identifier.
func (m *Manager) EvaluateMode(tenant, subjectID string) Mode {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg, ok := m.configs[tenant]
	if !ok || cfg.KillSwitchActive || cfg.TargetMode == ModeShadow || cfg.RolloutPercentage == 0 {
		return ModeShadow
	}

	if cfg.RolloutPercentage >= 100 {
		return cfg.TargetMode
	}

	// Deterministic bucket 0..99
	h := sha256.Sum256([]byte(tenant + ":" + subjectID))
	val := binary.BigEndian.Uint32(h[:4]) % 100

	if int(val) < cfg.RolloutPercentage {
		return cfg.TargetMode
	}
	return ModeShadow
}

// RecordOutcome updates friction counts and checks if emergency rollback ceiling is exceeded.
func (m *Manager) RecordOutcome(tenant string, hadFriction bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg, ok := m.configs[tenant]
	if !ok || cfg.KillSwitchActive || cfg.TargetMode == ModeShadow {
		return
	}

	m.assessments[tenant]++
	if hadFriction {
		m.frictions[tenant]++
	}

	total := m.assessments[tenant]
	friction := m.frictions[tenant]

	if total >= 50 {
		rate := float64(friction) / float64(total)
		if rate > cfg.FrictionCeiling {
			cfg.KillSwitchActive = true
			m.autoRollback[tenant] = fmt.Sprintf("Emergency rollback: friction rate %.2f%% exceeded ceiling %.2f%% over %d events", rate*100, cfg.FrictionCeiling*100, total)
		}
	}

	if total >= cfg.FrictionWindow {
		m.assessments[tenant] = 0
		m.frictions[tenant] = 0
	}
}

func (m *Manager) GetAutoRollbackReason(tenant string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.autoRollback[tenant]
}
