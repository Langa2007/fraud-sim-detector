package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
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

func canonical(r Record) []byte {
	r.Hash = ""
	b, _ := json.Marshal(r)
	return b
}

func main() {
	verbose := flag.Bool("v", false, "verbose output for each record")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Kinga Standalone Evidence Chain Verifier\n")
		fmt.Fprintf(os.Stderr, "Usage: evidence-verify [-v] <path/to/evidence.jsonl>\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		os.Exit(1)
	}

	path := args[0]
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening evidence file %s: %v\n", path, err)
		os.Exit(1)
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	var count uint64
	previous := ""

	for {
		var r Record
		err := dec.Decode(&r)
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "FAIL: Corrupted JSON at record #%d: %v\n", count+1, err)
			os.Exit(1)
		}

		count++
		if r.Sequence != count {
			fmt.Fprintf(os.Stderr, "FAIL: Sequence mismatch at record #%d (got sequence %d)\n", count, r.Sequence)
			os.Exit(1)
		}

		if r.PreviousHash != previous {
			fmt.Fprintf(os.Stderr, "FAIL: Broken hash chain at sequence #%d (got previous_hash=%s, expected=%s)\n",
				count, r.PreviousHash, previous)
			os.Exit(1)
		}

		sum := sha256.Sum256(canonical(r))
		computedHash := hex.EncodeToString(sum[:])
		if computedHash != r.Hash {
			fmt.Fprintf(os.Stderr, "FAIL: Tampered record content at sequence #%d (stored hash=%s, computed hash=%s)\n",
				count, r.Hash, computedHash)
			os.Exit(1)
		}

		if *verbose {
			fmt.Printf("[%d] %s (tenant=%s, event=%s, hash=%.12s...)\n",
				r.Sequence, r.EvidenceID, r.Tenant, r.TenantEventID, r.Hash)
		}

		previous = r.Hash
	}

	if count == 0 {
		fmt.Printf("Evidence log %s is EMPTY (0 records).\n", path)
		return
	}

	fmt.Printf("OK: Verified %d evidence records successfully.\n", count)
	fmt.Printf("    Head Hash: %s\n", previous)
}
