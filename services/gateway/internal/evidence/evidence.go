package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Record struct {
	Sequence        uint64          `json:"sequence"`
	EvidenceID      string          `json:"evidence_id"`
	Tenant          string          `json:"tenant"`
	TenantEventID   string          `json:"tenant_event_id"`
	RecordedAt      time.Time       `json:"recorded_at"`
	EventHash       string          `json:"event_hash"`
	FeatureSnapshot json.RawMessage `json:"feature_snapshot"`
	Decision        json.RawMessage `json:"decision"`
	PreviousHash    string          `json:"previous_hash"`
	Hash            string          `json:"hash"`
}
type Log struct {
	mu     sync.Mutex
	f      *os.File
	path   string
	seq    uint64
	head   string
	failed error
}

func Open(path string) (*Log, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	l := &Log{f: f, path: path}
	if e = l.verifyExisting(); e != nil {
		f.Close()
		return nil, e
	}
	return l, nil
}
func (l *Log) Verify() (uint64, string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.Open(l.path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	var count uint64
	previous := ""
	for {
		var r Record
		err = dec.Decode(&r)
		if err == io.EOF {
			return count, previous, nil
		}
		if err != nil {
			return count, previous, err
		}
		count++
		if r.Sequence != count || r.PreviousHash != previous {
			return count, previous, fmt.Errorf("chain linkage mismatch at sequence %d", count)
		}
		sum := sha256.Sum256(canonical(r))
		if hex.EncodeToString(sum[:]) != r.Hash {
			return count, previous, fmt.Errorf("record hash mismatch at sequence %d", count)
		}
		previous = r.Hash
	}
}

// Find returns a tenant-scoped evidence record after verifying the entire chain.
func (l *Log) Find(tenant, evidenceID string) (Record, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.Open(l.path)
	if err != nil {
		return Record{}, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	var count uint64
	previous := ""
	var found *Record
	for {
		var record Record
		err = dec.Decode(&record)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Record{}, err
		}
		count++
		if record.Sequence != count || record.PreviousHash != previous {
			return Record{}, fmt.Errorf("evidence chain linkage mismatch at sequence %d", count)
		}
		sum := sha256.Sum256(canonical(record))
		if hex.EncodeToString(sum[:]) != record.Hash {
			return Record{}, fmt.Errorf("evidence chain hash mismatch at sequence %d", count)
		}
		if record.Tenant == tenant && record.EvidenceID == evidenceID {
			copy := record
			found = &copy
		}
		previous = record.Hash
	}
	if found == nil {
		return Record{}, os.ErrNotExist
	}
	return *found, nil
}
func canonical(r Record) []byte { r.Hash = ""; b, _ := json.Marshal(r); return b }
func (l *Log) verifyExisting() error {
	if _, e := l.f.Seek(0, 0); e != nil {
		return e
	}
	dec := json.NewDecoder(l.f)
	prev := ""
	var n uint64
	for {
		var r Record
		e := dec.Decode(&r)
		if e != nil {
			if errors.Is(e, io.EOF) {
				break
			}
			return fmt.Errorf("evidence log decode: %w", e)
		}
		n++
		if r.Sequence != n || r.PreviousHash != prev {
			return fmt.Errorf("evidence chain broken at sequence %d", n)
		}
		sum := sha256.Sum256(canonical(r))
		if hex.EncodeToString(sum[:]) != r.Hash {
			return fmt.Errorf("evidence record hash mismatch at sequence %d", n)
		}
		prev = r.Hash
	}
	l.seq = n
	l.head = prev
	_, e := l.f.Seek(0, 2)
	return e
}
func (l *Log) Append(tenant, id string, event, features []byte, decision func(string) ([]byte, error)) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.failed != nil {
		return "", fmt.Errorf("evidence writer is failed: %w", l.failed)
	}
	eidHash := sha256.Sum256([]byte(tenant + "\x00" + id))
	eid := fmt.Sprintf("ev_%d_%x", l.seq+1, eidHash[:8])
	decisionBytes, err := decision(eid)
	if err != nil {
		return "", err
	}
	eh := sha256.Sum256(event)
	r := Record{l.seq + 1, eid, tenant, id, time.Now().UTC(), hex.EncodeToString(eh[:]), append(json.RawMessage(nil), features...), append(json.RawMessage(nil), decisionBytes...), l.head, ""}
	sum := sha256.Sum256(canonical(r))
	r.Hash = hex.EncodeToString(sum[:])
	b, e := json.Marshal(r)
	if e != nil {
		return "", e
	}
	if _, e = l.f.Write(append(b, '\n')); e != nil {
		l.failed = e
		return "", e
	}
	if e = l.f.Sync(); e != nil {
		l.failed = e
		return "", e
	}
	l.seq++
	l.head = r.Hash
	return eid, nil
}
func (l *Log) Close() error { return l.f.Close() }
