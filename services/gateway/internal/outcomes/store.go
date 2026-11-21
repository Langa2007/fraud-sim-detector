package outcomes

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var ErrUnavailable = errors.New("outcome ledger unavailable")

type Submission struct {
	SubmissionID  string    `json:"submission_id"`
	TenantEventID string    `json:"tenant_event_id"`
	EvidenceID    string    `json:"evidence_id"`
	Label         string    `json:"label"`
	Reviewer      string    `json:"reviewer"`
	Notes         string    `json:"notes,omitempty"`
	LossKES       *float64  `json:"loss_kes,omitempty"`
	ObservedAt    time.Time `json:"observed_at"`
}

type Record struct {
	Sequence     uint64     `json:"sequence"`
	Tenant       string     `json:"tenant"`
	Submission   Submission `json:"submission"`
	RecordedAt   time.Time  `json:"recorded_at"`
	PreviousHash string     `json:"previous_hash"`
	Hash         string     `json:"hash"`
}

type Store struct {
	mu           sync.Mutex
	file         *os.File
	sequence     uint64
	head         string
	bySubmission map[string]Record
	failed       error
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	s := &Store{file: f, bySubmission: map[string]Record{}}
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
		var record Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return fmt.Errorf("decode outcome ledger: %w", err)
		}
		if record.Sequence != s.sequence+1 || record.PreviousHash != s.head || record.Tenant == "" || record.Submission.SubmissionID == "" {
			return fmt.Errorf("outcome ledger linkage or sequence invalid at record %d", record.Sequence)
		}
		if recordHash(record) != record.Hash {
			return fmt.Errorf("outcome ledger hash mismatch at record %d", record.Sequence)
		}
		s.bySubmission[submissionKey(record.Tenant, record.Submission.SubmissionID)] = record
		s.sequence, s.head = record.Sequence, record.Hash
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read outcome ledger: %w", err)
	}
	_, err := s.file.Seek(0, 2)
	return err
}

func (s *Store) Append(tenant string, submission Submission) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed != nil {
		return Record{}, fmt.Errorf("%w: %v", ErrUnavailable, s.failed)
	}
	key := submissionKey(tenant, submission.SubmissionID)
	if existing, ok := s.bySubmission[key]; ok {
		oldBytes, _ := json.Marshal(existing.Submission)
		newBytes, _ := json.Marshal(submission)
		if string(oldBytes) == string(newBytes) {
			return existing, nil
		}
		return Record{}, errors.New("submission_id_reused_with_different_outcome")
	}
	record := Record{Sequence: s.sequence + 1, Tenant: tenant, Submission: submission, RecordedAt: time.Now().UTC(), PreviousHash: s.head}
	record.Hash = recordHash(record)
	encoded, err := json.Marshal(record)
	if err != nil {
		return Record{}, err
	}
	if _, err = s.file.Write(append(encoded, '\n')); err != nil {
		s.failed = err
		return Record{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if err = s.file.Sync(); err != nil {
		s.failed = err
		return Record{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	s.sequence, s.head = record.Sequence, record.Hash
	s.bySubmission[key] = record
	return record, nil
}

func recordHash(record Record) string {
	record.Hash = ""
	encoded, _ := json.Marshal(record)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func submissionKey(tenant, id string) string { return tenant + "\x00" + id }

func (s *Store) Close() error { return s.file.Close() }
