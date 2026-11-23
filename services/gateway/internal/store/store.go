package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

var ErrConflict = fmt.Errorf("idempotency key reused with a different payload")

type Entry struct {
	BodyHash string          `json:"body_hash"`
	Response json.RawMessage `json:"response"`
	Expires  time.Time       `json:"expires"`
}
type Store struct {
	mu      sync.Mutex
	entries map[string]Entry
	ttl     time.Duration
}

// IdempotencyStore is the persistence contract used by the assessment gateway.
type IdempotencyStore interface {
	Get(key string, body []byte) (json.RawMessage, bool, error)
	Put(key string, body, response []byte) error
}

func New(ttl string) *Store {
	d, e := time.ParseDuration(ttl)
	if e != nil || d <= 0 {
		d = 24 * time.Hour
	}
	return &Store{entries: map[string]Entry{}, ttl: d}
}
func Key(tenant, id string) string { return tenant + "\x00" + id }
func Hash(b []byte) string         { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func (s *Store) Get(k string, body []byte) (json.RawMessage, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.entries[k]
	if !ok {
		return nil, false, nil
	}
	if time.Now().After(v.Expires) {
		delete(s.entries, k)
		return nil, false, nil
	}
	if v.BodyHash != Hash(body) {
		return nil, false, ErrConflict
	}
	return v.Response, true, nil
}
func (s *Store) Put(k string, body, response []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[k] = Entry{Hash(body), append(json.RawMessage(nil), response...), time.Now().Add(s.ttl)}
	return nil
}
