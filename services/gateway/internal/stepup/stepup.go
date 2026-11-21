package stepup

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrNotFound       = errors.New("challenge not found")
	ErrAlreadyDecided = errors.New("challenge already resolved")
	ErrExpired        = errors.New("challenge expired")
)

type Channel string

const (
	ChannelUSSD  Channel = "ussd_push"
	ChannelSMS   Channel = "sms_callback"
	ChannelInApp Channel = "in_app"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusApproved  Status = "approved"
	StatusRejected  Status = "rejected"
	StatusExpired   Status = "expired"
	StatusEscalated Status = "escalated"
)

type Challenge struct {
	ChallengeID   string    `json:"challenge_id"`
	Tenant        string    `json:"tenant"`
	TenantEventID string    `json:"tenant_event_id"`
	Channel       Channel   `json:"channel"`
	Language      string    `json:"language"`
	Prompt        string    `json:"prompt"`
	Status        Status    `json:"status"`
	Amount        float64   `json:"amount"`
	Beneficiary   string    `json:"beneficiary"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	ResolvedAt    time.Time `json:"resolved_at,omitempty"`
	EscalatedTo   Channel   `json:"escalated_to,omitempty"`
}

type Orchestrator struct {
	mu         sync.Mutex
	challenges map[string]*Challenge
}

func NewOrchestrator() *Orchestrator {
	return &Orchestrator{
		challenges: make(map[string]*Challenge),
	}
}

func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "ch_" + hex.EncodeToString(b)
}

func FormatPrompt(ch Channel, lang string, amount float64, beneficiary string) string {
	if lang == "sw" {
		switch ch {
		case ChannelUSSD:
			return fmt.Sprintf("Uthibitisho wa Kinga: Thibitisha uhamishaji wa KSh %.0f kwenda %s. Bonyeza 1 kukubali, 2 kukataa.", amount, beneficiary)
		case ChannelSMS:
			return fmt.Sprintf("Kinga: Umeanzisha muamala wa KSh %.0f kwenda %s. Kama ni wewe, jibu NDIYO. Kama si wewe, jibu HAPANA mara moja.", amount, beneficiary)
		default:
			return fmt.Sprintf("Ombi la uthibitisho wa usalama wa Kinga limetumwa: KSh %.0f kwenda %s.", amount, beneficiary)
		}
	}

	// Default English
	switch ch {
	case ChannelUSSD:
		return fmt.Sprintf("Kinga Verification: Confirm transfer of KSh %.0f to %s. Press 1 to approve, 2 to reject.", amount, beneficiary)
	case ChannelSMS:
		return fmt.Sprintf("Kinga Security: A transfer of KSh %.0f to %s was initiated. Reply YES to authorize, or NO immediately if this was not you.", amount, beneficiary)
	default:
		return fmt.Sprintf("Kinga security challenge: Confirm transfer of KSh %.0f to %s.", amount, beneficiary)
	}
}

func (o *Orchestrator) Initiate(tenant, eventID string, ch Channel, lang string, amount float64, beneficiary string, ttl time.Duration) (*Challenge, error) {
	if ttl <= 0 {
		ttl = 2 * time.Minute
	}
	if lang != "sw" && lang != "en" {
		lang = "en"
	}
	if ch != ChannelUSSD && ch != ChannelSMS && ch != ChannelInApp {
		ch = ChannelUSSD
	}

	prompt := FormatPrompt(ch, lang, amount, beneficiary)
	now := time.Now().UTC()

	c := &Challenge{
		ChallengeID:   randomID(),
		Tenant:        tenant,
		TenantEventID: eventID,
		Channel:       ch,
		Language:      lang,
		Prompt:        prompt,
		Status:        StatusPending,
		Amount:        amount,
		Beneficiary:   beneficiary,
		CreatedAt:     now,
		ExpiresAt:     now.Add(ttl),
	}

	o.mu.Lock()
	defer o.mu.Unlock()
	o.challenges[c.ChallengeID] = c
	return c, nil
}

func (o *Orchestrator) Get(tenant, challengeID string) (*Challenge, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	c, ok := o.challenges[challengeID]
	if !ok || c.Tenant != tenant {
		return nil, ErrNotFound
	}

	// Check expiration
	if c.Status == StatusPending && time.Now().UTC().After(c.ExpiresAt) {
		c.Status = StatusExpired
		c.ResolvedAt = c.ExpiresAt
	}

	copy := *c
	return &copy, nil
}

func (o *Orchestrator) Respond(tenant, challengeID string, approve bool) (*Challenge, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	c, ok := o.challenges[challengeID]
	if !ok || c.Tenant != tenant {
		return nil, ErrNotFound
	}

	if c.Status != StatusPending {
		if c.Status == StatusPending && time.Now().UTC().After(c.ExpiresAt) {
			c.Status = StatusExpired
			c.ResolvedAt = c.ExpiresAt
			return nil, ErrExpired
		}
		return nil, ErrAlreadyDecided
	}

	if time.Now().UTC().After(c.ExpiresAt) {
		c.Status = StatusExpired
		c.ResolvedAt = c.ExpiresAt
		return nil, ErrExpired
	}

	now := time.Now().UTC()
	c.ResolvedAt = now
	if approve {
		c.Status = StatusApproved
	} else {
		c.Status = StatusRejected
	}

	copy := *c
	return &copy, nil
}

func (o *Orchestrator) Escalate(tenant, challengeID string, nextChannel Channel) (*Challenge, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	c, ok := o.challenges[challengeID]
	if !ok || c.Tenant != tenant {
		return nil, ErrNotFound
	}

	now := time.Now().UTC()
	c.Status = StatusEscalated
	c.EscalatedTo = nextChannel
	c.ResolvedAt = now

	// Open a new challenge for the escalated channel
	newChallenge := &Challenge{
		ChallengeID:   randomID(),
		Tenant:        tenant,
		TenantEventID: c.TenantEventID,
		Channel:       nextChannel,
		Language:      c.Language,
		Prompt:        FormatPrompt(nextChannel, c.Language, c.Amount, c.Beneficiary),
		Status:        StatusPending,
		Amount:        c.Amount,
		Beneficiary:   c.Beneficiary,
		CreatedAt:     now,
		ExpiresAt:     now.Add(3 * time.Minute),
	}
	o.challenges[newChallenge.ChallengeID] = newChallenge
	copy := *newChallenge
	return &copy, nil
}
