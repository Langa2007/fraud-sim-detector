package cases

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"kinga/services/gateway/internal/engine"
)

var ErrUnavailable = errors.New("case ledger unavailable")

type Action struct {
	ActionID  string    `json:"action_id"`
	Type      string    `json:"type"`
	Reviewer  string    `json:"reviewer"`
	Assignee  string    `json:"assignee,omitempty"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Case struct {
	CaseID        string          `json:"case_id"`
	Tenant        string          `json:"tenant"`
	TenantEventID string          `json:"tenant_event_id"`
	EvidenceID    string          `json:"evidence_id"`
	Decision      string          `json:"decision"`
	RiskScore     float64         `json:"risk_score"`
	RiskBand      string          `json:"risk_band"`
	PolicyVersion string          `json:"policy_version"`
	ModelVersion  *string         `json:"model_version"`
	Reasons       []engine.Reason `json:"reasons"`
	Status        string          `json:"status"`
	Assignee      string          `json:"assignee,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	History       []Action        `json:"history"`
}

type entry struct {
	Sequence     uint64 `json:"sequence"`
	Case         Case   `json:"case"`
	PreviousHash string `json:"previous_hash"`
	Hash         string `json:"hash"`
}

type Store struct {
	mu      sync.Mutex
	file    *os.File
	seq     uint64
	head    string
	cases   map[string]Case
	actions map[string]string
	failed  error
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	s := &Store{file: f, cases: map[string]Case{}, actions: map[string]string{}}
	if err := s.load(); err != nil {
		_ = f.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	if _, err := s.file.Seek(0, 0); err != nil {
		return err
	}
	scanner := bufio.NewScanner(s.file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var item entry
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			return fmt.Errorf("decode case ledger: %w", err)
		}
		if item.Sequence != s.seq+1 || item.PreviousHash != s.head || item.Case.CaseID == "" || item.Case.Tenant == "" || hashEntry(item) != item.Hash {
			return fmt.Errorf("case ledger linkage or hash invalid at record %d", item.Sequence)
		}
		key := caseKey(item.Case.Tenant, item.Case.CaseID)
		for _, action := range item.Case.History {
			s.actions[actionKey(item.Case.Tenant, action.ActionID)] = actionDigest(item.Case.CaseID, action)
		}
		s.cases[key] = item.Case
		s.seq, s.head = item.Sequence, item.Hash
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read case ledger: %w", err)
	}
	_, err := s.file.Seek(0, 2)
	return err
}

func (s *Store) OpenCase(tenant string, result engine.Result) (Case, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := caseKey(tenant, result.EvidenceID)
	if existing, ok := s.cases[key]; ok {
		return existing, nil
	}
	now := time.Now().UTC()
	c := Case{CaseID: result.EvidenceID, Tenant: tenant, TenantEventID: result.TenantEventID,
		EvidenceID: result.EvidenceID, Decision: result.Decision, RiskScore: result.RiskScore,
		RiskBand: result.RiskBand, PolicyVersion: result.PolicyVersion, ModelVersion: result.ModelVersion,
		Reasons: append([]engine.Reason(nil), result.Reasons...), Status: "open", CreatedAt: now, UpdatedAt: now,
		History: []Action{}}
	if err := s.append(c); err != nil {
		return Case{}, err
	}
	s.cases[key] = c
	return c, nil
}

func (s *Store) Apply(tenant, caseID string, action Action) (Case, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := caseKey(tenant, caseID)
	c, ok := s.cases[key]
	if !ok {
		return Case{}, os.ErrNotExist
	}
	action.CreatedAt = time.Time{}
	digest := actionDigest(caseID, action)
	if old, exists := s.actions[actionKey(tenant, action.ActionID)]; exists {
		if old == digest {
			return c, nil
		}
		return Case{}, errors.New("action_id_reused_with_different_action")
	}
	if c.Status == "released" || c.Status == "held" {
		return Case{}, errors.New("case_already_resolved")
	}
	action.CreatedAt = time.Now().UTC()
	switch action.Type {
	case "assign":
		if action.Assignee == "" {
			return Case{}, errors.New("assignee_required")
		}
		c.Assignee, c.Status = action.Assignee, "assigned"
	case "note":
		if action.Note == "" {
			return Case{}, errors.New("note_required")
		}
	case "release":
		c.Status = "released"
	case "hold":
		c.Status = "held"
	default:
		return Case{}, errors.New("unsupported_case_action")
	}
	c.History = append(c.History, action)
	c.UpdatedAt = action.CreatedAt
	if err := s.append(c); err != nil {
		return Case{}, err
	}
	s.cases[key], s.actions[actionKey(tenant, action.ActionID)] = c, digest
	return c, nil
}

func (s *Store) Get(tenant, caseID string) (Case, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.cases[caseKey(tenant, caseID)]
	if !ok {
		return Case{}, os.ErrNotExist
	}
	return c, nil
}

func (s *Store) List(tenant, status string, limit int) []Case {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit < 1 || limit > 100 {
		limit = 50
	}
	result := make([]Case, 0)
	for _, c := range s.cases {
		if c.Tenant == tenant && (status == "" || c.Status == status) {
			result = append(result, c)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UpdatedAt.After(result[j].UpdatedAt) })
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

func (s *Store) append(c Case) error {
	if s.failed != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, s.failed)
	}
	item := entry{Sequence: s.seq + 1, Case: c, PreviousHash: s.head}
	item.Hash = hashEntry(item)
	encoded, err := json.Marshal(item)
	if err != nil {
		return err
	}
	if _, err = s.file.Write(append(encoded, '\n')); err != nil {
		s.failed = err
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if err = s.file.Sync(); err != nil {
		s.failed = err
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	s.seq, s.head = item.Sequence, item.Hash
	return nil
}

func hashEntry(item entry) string {
	item.Hash = ""
	b, _ := json.Marshal(item)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func actionDigest(caseID string, action Action) string {
	action.CreatedAt = time.Time{}
	b, _ := json.Marshal(struct {
		CaseID string `json:"case_id"`
		Action Action `json:"action"`
	}{caseID, action})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func caseKey(tenant, id string) string   { return tenant + "\x00" + id }
func actionKey(tenant, id string) string { return tenant + "\x00" + id }
func (s *Store) Close() error            { return s.file.Close() }

func ValidateAction(a Action) error {
	if a.ActionID == "" || len(a.ActionID) > 128 || strings.TrimSpace(a.Reviewer) == "" || len(a.Reviewer) > 128 || len(a.Assignee) > 128 || len(a.Note) > 1000 {
		return errors.New("required_case_action_fields_missing_or_invalid")
	}
	if (a.Type == "release" || a.Type == "hold") && strings.TrimSpace(a.Note) == "" {
		return errors.New("case_disposition_requires_reason")
	}
	return nil
}
