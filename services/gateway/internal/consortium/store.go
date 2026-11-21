// Package consortium stores privacy-preserving, token-based member indicators.
// Raw identifiers and member identities are never included in query responses.
package consortium

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

var tokenPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var ErrNotFound = errors.New("indicator_not_found")

type Indicator struct {
	ID         string    `json:"indicator_id"`
	Namespace  string    `json:"namespace"`
	Token      string    `json:"token"`
	Confidence float64   `json:"confidence"`
	ExpiresAt  time.Time `json:"expires_at"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

type Record struct {
	Sequence     uint64    `json:"sequence"`
	Action       string    `json:"action"`
	Tenant       string    `json:"tenant"`
	Indicator    Indicator `json:"indicator"`
	Reason       string    `json:"reason,omitempty"`
	RecordedAt   time.Time `json:"recorded_at"`
	PreviousHash string    `json:"previous_hash"`
	Hash         string    `json:"hash"`
}

type Query struct {
	Namespace                string    `json:"namespace"`
	Token                    string    `json:"token"`
	Confidence               float64   `json:"confidence"`
	IndependentConfirmations int       `json:"independent_confirmations"`
	ExpiresAt                time.Time `json:"expires_at"`
}

type Store struct {
	mu            sync.Mutex
	file          *os.File
	sequence      uint64
	head          string
	latest        map[string]Record
	contributors  map[string]map[string]bool
	contributions map[string]map[string]Indicator
	statuses      map[string]string
	memberCounts  map[string]uint64
	minimum       int
	failed        error
}

func Open(path string, minimumIndependent int) (*Store, error) {
	if minimumIndependent < 2 {
		return nil, errors.New("minimum independent confirmations must be at least 2")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	s := &Store{file: f, minimum: minimumIndependent, latest: map[string]Record{}, contributors: map[string]map[string]bool{}, contributions: map[string]map[string]Indicator{}, statuses: map[string]string{}, memberCounts: map[string]uint64{}}
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
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		var r Record
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			return fmt.Errorf("decode consortium ledger: %w", err)
		}
		if r.Sequence != s.sequence+1 || r.PreviousHash != s.head || r.Hash != hashRecord(r) {
			return fmt.Errorf("consortium ledger linkage invalid at record %d", r.Sequence)
		}
		key := indicatorKey(r.Indicator.Namespace, r.Indicator.Token)
		switch r.Action {
		case "contribute":
			if s.contributors[key] == nil {
				s.contributors[key] = map[string]bool{}
			}
			s.contributors[key][r.Tenant] = true
			if s.contributions[key] == nil {
				s.contributions[key] = map[string]Indicator{}
			}
			s.contributions[key][r.Tenant] = r.Indicator
			s.memberCounts[r.Tenant]++
			s.latest[key] = r
			if s.statuses[key] == "" {
				s.statuses[key] = "active"
			}
		case "dispute", "remove", "reinstate":
			s.latest[key] = r
			s.statuses[key] = r.Indicator.Status
		default:
			return fmt.Errorf("unknown consortium ledger action %q", r.Action)
		}
		s.sequence, s.head = r.Sequence, r.Hash
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	_, err := s.file.Seek(0, 2)
	return err
}

func (s *Store) Contribute(tenant, namespace, token string, confidence float64, ttl time.Duration) (Record, error) {
	if tenant == "" || !validNamespace(namespace) || !tokenPattern.MatchString(token) || confidence < 0 || confidence > 1 || ttl <= 0 || ttl > 90*24*time.Hour {
		return Record{}, errors.New("invalid_indicator")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := indicatorKey(namespace, token)
	if s.failed != nil {
		return Record{}, fmt.Errorf("consortium ledger unavailable: %w", s.failed)
	}
	if s.contributors[key][tenant] {
		return Record{}, errors.New("member_already_contributed")
	}
	if status := s.statuses[key]; status != "" && status != "active" {
		return Record{}, errors.New("indicator_not_active")
	}
	now := time.Now().UTC()
	record := Record{Action: "contribute", Tenant: tenant, Indicator: Indicator{ID: fmt.Sprintf("ci-%x", sha256.Sum256([]byte(key+tenant+now.String()))), Namespace: namespace, Token: token, Confidence: confidence, ExpiresAt: now.Add(ttl), Status: "active", CreatedAt: now}, RecordedAt: now}
	if err := s.appendLocked(&record); err != nil {
		return Record{}, err
	}
	if s.contributors[key] == nil {
		s.contributors[key] = map[string]bool{}
	}
	s.contributors[key][tenant] = true
	if s.contributions[key] == nil {
		s.contributions[key] = map[string]Indicator{}
	}
	s.contributions[key][tenant] = record.Indicator
	s.latest[key] = record
	if s.statuses[key] == "" {
		s.statuses[key] = "active"
	}
	s.memberCounts[tenant]++
	return record, nil
}

func (s *Store) Query(namespace, token string, now time.Time) (Query, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := indicatorKey(namespace, token)
	if s.statuses[key] != "active" {
		return Query{}, false
	}
	// Confidence decays exponentially with a 30 day half-life.
	var confidence float64
	var confirmations int
	var expiresAt time.Time
	for _, contribution := range s.contributions[key] {
		if !contribution.ExpiresAt.After(now) {
			continue
		}
		age := now.Sub(contribution.CreatedAt).Hours()
		confidence += contribution.Confidence * mathExp2(-age/(24*30))
		confirmations++
		if contribution.ExpiresAt.After(expiresAt) {
			expiresAt = contribution.ExpiresAt
		}
	}
	if confirmations < s.minimum {
		return Query{}, false
	}
	return Query{Namespace: namespace, Token: token, Confidence: confidence / float64(confirmations), IndependentConfirmations: confirmations, ExpiresAt: expiresAt}, true
}

func (s *Store) Dispute(tenant, namespace, token, reason string) (Record, error) {
	if tenant == "" || !validNamespace(namespace) || !tokenPattern.MatchString(token) || len(reason) < 8 || len(reason) > 500 {
		return Record{}, errors.New("invalid_dispute")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := indicatorKey(namespace, token)
	r, ok := s.latest[key]
	if !ok {
		return Record{}, ErrNotFound
	}
	if !s.contributors[key][tenant] {
		return Record{}, errors.New("only_contributors_may_dispute")
	}
	now := time.Now().UTC()
	r.Action = "dispute"
	r.Tenant = tenant
	r.Reason = reason
	r.RecordedAt = now
	r.Indicator.Status = "disputed"
	if err := s.appendLocked(&r); err != nil {
		return Record{}, err
	}
	s.latest[key] = r
	s.statuses[key] = r.Indicator.Status
	return r, nil
}

func (s *Store) Resolve(namespace, token, action, reason string) (Record, error) {
	if !validNamespace(namespace) || !tokenPattern.MatchString(token) || (action != "remove" && action != "reinstate") || len(reason) < 8 || len(reason) > 500 {
		return Record{}, errors.New("invalid_resolution")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := indicatorKey(namespace, token)
	r, ok := s.latest[key]
	if !ok {
		return Record{}, ErrNotFound
	}
	now := time.Now().UTC()
	r.Action = action
	r.Tenant = "governance"
	r.Reason = reason
	r.RecordedAt = now
	if action == "remove" {
		r.Indicator.Status = "removed"
	} else {
		r.Indicator.Status = "active"
	}
	if err := s.appendLocked(&r); err != nil {
		return Record{}, err
	}
	s.latest[key] = r
	s.statuses[key] = r.Indicator.Status
	return r, nil
}

func (s *Store) MemberContributionCount(tenant string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.memberCounts[tenant]
}
func (s *Store) appendLocked(r *Record) error {
	if s.failed != nil {
		return s.failed
	}
	r.Sequence = s.sequence + 1
	r.PreviousHash = s.head
	r.Hash = hashRecord(*r)
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if _, err = s.file.Write(append(b, '\n')); err != nil {
		s.failed = err
		return err
	}
	if err = s.file.Sync(); err != nil {
		s.failed = err
		return err
	}
	s.sequence, s.head = r.Sequence, r.Hash
	return nil
}
func hashRecord(r Record) string {
	r.Hash = ""
	b, _ := json.Marshal(r)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func indicatorKey(namespace, token string) string { return namespace + "\x00" + token }
func validNamespace(s string) bool {
	return s == "msisdn" || s == "account" || s == "device" || s == "beneficiary" || s == "agent" || s == "wallet"
}
func mathExp2(x float64) float64 { return math.Pow(2, x) }
func (s *Store) Close() error    { return s.file.Close() }
