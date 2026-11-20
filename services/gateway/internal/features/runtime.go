package features

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"kinga/services/gateway/internal/engine"
)

type transfer struct {
	at             time.Time
	newBeneficiary bool
}
type accountState struct {
	lastSeen      time.Time
	devices       map[string]time.Time
	beneficiaries map[string]time.Time
	transfers     []transfer
}
type Runtime struct {
	mu       sync.Mutex
	accounts map[string]*accountState
	key      []byte
}

func NewRuntime() *Runtime {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("cannot initialize feature token key: " + err.Error())
	}
	return &Runtime{accounts: map[string]*accountState{}, key: key}
}
func (r *Runtime) token(tenant, value string) string {
	h := hmac.New(sha256.New, r.key)
	_, _ = h.Write([]byte(tenant + "\x00" + value))
	return hex.EncodeToString(h.Sum(nil))
}

// Observe computes first-seen and 15-minute velocity features before updating history.
// Identifiers are held only as tenant-scoped one-way hashes in this development hot store.
func (r *Runtime) Observe(tenant string, event *engine.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	subject := event.Subject.AccountRef
	if subject == "" {
		subject = event.Subject.MSISDN
	}
	if subject == "" {
		return
	}
	key := r.token(tenant, subject)
	state := r.accounts[key]
	if state == nil || event.OccurredAt.Sub(state.lastSeen) > 90*24*time.Hour || event.OccurredAt.Before(state.lastSeen.Add(-15*time.Minute)) {
		state = &accountState{devices: map[string]time.Time{}, beneficiaries: map[string]time.Time{}}
		r.accounts[key] = state
	}
	now := event.OccurredAt
	state.lastSeen = now
	for id, seenAt := range state.devices {
		if now.Sub(seenAt) > 90*24*time.Hour {
			delete(state.devices, id)
		}
	}
	for id, seenAt := range state.beneficiaries {
		if now.Sub(seenAt) > 90*24*time.Hour {
			delete(state.beneficiaries, id)
		}
	}
	event.Signals.DeviceFirstSeen = false
	if event.Device.DeviceID != "" {
		id := r.token(tenant, event.Device.DeviceID)
		_, seen := state.devices[id]
		event.Signals.DeviceFirstSeen = !seen
		state.devices[id] = now
	}
	event.Signals.BeneficiaryFirstSeen = false
	beneficiaryID := event.Transaction.Beneficiary.Ref
	newBeneficiary := false
	if beneficiaryID != "" {
		id := r.token(tenant, beneficiaryID)
		_, seen := state.beneficiaries[id]
		newBeneficiary = !seen
		event.Signals.BeneficiaryFirstSeen = newBeneficiary
		state.beneficiaries[id] = now
	}
	if event.EventType != "funds_transfer" {
		return
	}
	cutoff := now.Add(-15 * time.Minute)
	recent := state.transfers[:0]
	for _, t := range state.transfers {
		if !t.at.Before(cutoff) && !t.at.After(now) {
			recent = append(recent, t)
		}
	}
	state.transfers = recent
	state.transfers = append(state.transfers, transfer{now, newBeneficiary})
	event.Signals.TransfersInWindow = len(state.transfers)
	newCount := 0
	for _, t := range state.transfers {
		if t.newBeneficiary {
			newCount++
		}
	}
	event.Signals.NewBeneficiariesInWindow = newCount
	if len(state.transfers) > 1 {
		event.Signals.WindowMinutes = int(now.Sub(state.transfers[0].at).Minutes())
	} else {
		event.Signals.WindowMinutes = 0
	}
}
