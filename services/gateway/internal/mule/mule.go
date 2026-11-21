package mule

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"kinga/services/gateway/internal/engine"
)

// Anomaly describes an identified agent/wallet mule risk or structuring anomaly.
type Anomaly struct {
	IsAnomaly bool    `json:"is_anomaly"`
	Code      string  `json:"code,omitempty"`
	Reason    string  `json:"reason,omitempty"`
	Score     float64 `json:"score,omitempty"`
}

type txEvent struct {
	amount     float64
	occurredAt time.Time
}

type agentState struct {
	recentTxs []txEvent
	lastSeen  time.Time
}

// Detector identifies agent float movement anomalies, structuring (smurfing),
// and collusive mule cash-out patterns.
type Detector struct {
	mu     sync.Mutex
	agents map[string]*agentState
}

// New creates a new mule and float risk detector.
func New() *Detector {
	return &Detector{
		agents: make(map[string]*agentState),
	}
}

// Evaluate analyzes the event for mule or float movement anomalies.
func (d *Detector) Evaluate(tenant string, ev *engine.Event) Anomaly {
	if ev == nil {
		return Anomaly{}
	}

	isAgentEvent := ev.EventType == "agent_float_movement" || ev.Channel == "agent"
	agentRef := ev.Subject.AccountRef
	if agentRef == "" {
		agentRef = ev.Subject.MSISDN
	}

	// 1. Rapid Float Depletion:
	// Float moved / drained > 80% within <= 30 minutes
	if isAgentEvent && ev.Signals.BalanceDrainRatio != nil && *ev.Signals.BalanceDrainRatio >= 0.80 {
		windowMin := ev.Signals.WindowMinutes
		if windowMin == 0 {
			windowMin = 30
		}
		if windowMin <= 30 {
			return Anomaly{
				IsAnomaly: true,
				Code:      "AGENT_FLOAT_DEPLETION",
				Reason:    fmt.Sprintf("Agent float depleted by %.0f%% within %d minutes", *ev.Signals.BalanceDrainRatio*100, windowMin),
				Score:     0.85,
			}
		}
	}

	// 2. Structuring / Smurfing Detection:
	// Transfers deliberately kept just below mandatory reporting thresholds (e.g. 90,000 - 99,999 KES)
	currency := strings.ToUpper(ev.Transaction.Currency)
	if currency == "" {
		currency = "KES"
	}
	amount := ev.Transaction.Amount

	d.mu.Lock()
	defer d.mu.Unlock()

	agentKey := tenant + ":" + agentRef
	state := d.agents[agentKey]
	if state == nil {
		state = &agentState{recentTxs: make([]txEvent, 0, 8)}
		d.agents[agentKey] = state
	}

	now := ev.OccurredAt
	if now.IsZero() {
		now = time.Now().UTC()
	}

	// Prune events older than 1 hour
	cutoff := now.Add(-1 * time.Hour)
	valid := state.recentTxs[:0]
	for _, tx := range state.recentTxs {
		if tx.occurredAt.After(cutoff) {
			valid = append(valid, tx)
		}
	}
	state.recentTxs = valid
	state.recentTxs = append(state.recentTxs, txEvent{amount: amount, occurredAt: now})
	state.lastSeen = now

	// Check single-transaction structuring threshold:
	isStructuringBand := (currency == "KES" && amount >= 90000 && amount < 100000)
	if isStructuringBand {
		// Count how many structured transactions occurred in the last hour
		structuredCount := 0
		for _, tx := range state.recentTxs {
			if tx.amount >= 90000 && tx.amount < 100000 {
				structuredCount++
			}
		}
		if structuredCount >= 2 || isAgentEvent {
			return Anomaly{
				IsAnomaly: true,
				Code:      "STRUCTURING_DETECTED",
				Reason:    fmt.Sprintf("Transaction amount (%.2f %s) structured just below reporting threshold with %d occurrences in 1h window", amount, currency, structuredCount),
				Score:     0.78,
			}
		}
	}

	// 3. Agent Burst Cash-Out / Mule Funnel:
	// Rapid outbound transfer bursts through an agent or newly observed agent account
	if isAgentEvent && ev.Signals.TransfersInWindow >= 5 && ev.Signals.RapidOutbound {
		return Anomaly{
			IsAnomaly: true,
			Code:      "AGENT_BURST_CASH_OUT",
			Reason:    fmt.Sprintf("Agent processed %d rapid outbound transactions in window", ev.Signals.TransfersInWindow),
			Score:     0.82,
		}
	}

	return Anomaly{IsAnomaly: false}
}
