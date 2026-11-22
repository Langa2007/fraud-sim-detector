package main

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type statement struct {
	Sequence   uint64    `json:"sequence"`
	Head       string    `json:"head"`
	AnchoredAt time.Time `json:"anchored_at"`
	KeyID      string    `json:"key_id"`
}

type anchor struct {
	statement
	Signature string `json:"signature"`
}

type evidenceRecord struct {
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

func main() {
	if len(os.Args) < 2 {
		fatal("usage: evidence-anchor <keygen|anchor|verify> [flags]")
	}
	switch os.Args[1] {
	case "keygen":
		keygenCommand(os.Args[2:])
	case "anchor":
		anchorCommand(os.Args[2:])
	case "verify":
		verifyCommand(os.Args[2:])
	default:
		fatal("expected keygen, anchor, or verify subcommand")
	}
}

func keygenCommand(args []string) {
	flags := flag.NewFlagSet("keygen", flag.ExitOnError)
	privatePath := flags.String("private-key", "", "output raw Ed25519 private key file")
	publicPath := flags.String("public-key", "", "output raw Ed25519 public key file")
	_ = flags.Parse(args)
	if *privatePath == "" || *publicPath == "" {
		fatal("-private-key and -public-key are required")
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fatal(err.Error())
	}
	if err := writeNewFile(*privatePath, privateKey, 0600); err != nil {
		fatal("write private key: " + err.Error())
	}
	if err := writeNewFile(*publicPath, publicKey, 0644); err != nil {
		_ = os.Remove(*privatePath)
		fatal("write public key: " + err.Error())
	}
	fmt.Println("created offline Ed25519 keypair; store the private key in approved secret management")
}

func writeNewFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return err
	}
	return f.Close()
}

func anchorCommand(args []string) {
	flags := flag.NewFlagSet("anchor", flag.ExitOnError)
	logPath := flags.String("evidence", "data/evidence.jsonl", "evidence JSONL path")
	keyPath := flags.String("private-key", "", "Ed25519 private key file (raw or base64)")
	keyID := flags.String("key-id", "", "operator-assigned public key identifier")
	outPath := flags.String("output", "data/evidence-anchors.jsonl", "append-only anchor output")
	_ = flags.Parse(args)
	if *keyPath == "" || *keyID == "" {
		fatal("-private-key and -key-id are required")
	}
	sequence, head, err := verifyEvidence(*logPath)
	if err != nil {
		fatal("evidence verification failed: " + err.Error())
	}
	if sequence == 0 {
		fatal("refusing to anchor an empty evidence chain")
	}
	key, err := readKey(*keyPath, ed25519.PrivateKeySize)
	if err != nil {
		fatal("invalid private key: " + err.Error())
	}
	statement := statement{Sequence: sequence, Head: head, AnchoredAt: time.Now().UTC(), KeyID: *keyID}
	payload, _ := json.Marshal(statement)
	record := anchor{statement: statement, Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(ed25519.PrivateKey(key), payload))}
	encoded, err := json.Marshal(record)
	if err != nil {
		fatal(err.Error())
	}
	if err := os.MkdirAll(filepath.Dir(*outPath), 0700); err != nil {
		fatal(err.Error())
	}
	f, err := os.OpenFile(*outPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		fatal(err.Error())
	}
	if _, err = f.Write(append(encoded, '\n')); err != nil {
		_ = f.Close()
		fatal(err.Error())
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		fatal(err.Error())
	}
	if err = f.Close(); err != nil {
		fatal(err.Error())
	}
	fmt.Printf("anchored evidence sequence %d head %s key %s\n", sequence, head, *keyID)
}

func verifyEvidence(path string) (uint64, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	decoder := json.NewDecoder(f)
	var count uint64
	previous := ""
	for {
		var record evidenceRecord
		err := decoder.Decode(&record)
		if err == io.EOF {
			return count, previous, nil
		}
		if err != nil {
			return count, previous, err
		}
		count++
		if record.Sequence != count || record.PreviousHash != previous {
			return count, previous, fmt.Errorf("chain linkage mismatch at sequence %d", count)
		}
		expected := record.Hash
		record.Hash = ""
		canonical, err := json.Marshal(record)
		if err != nil {
			return count, previous, err
		}
		sum := sha256.Sum256(canonical)
		if hex.EncodeToString(sum[:]) != expected {
			return count, previous, fmt.Errorf("record hash mismatch at sequence %d", count)
		}
		previous = expected
	}
}

func verifyCommand(args []string) {
	flags := flag.NewFlagSet("verify", flag.ExitOnError)
	anchorsPath := flags.String("anchors", "data/evidence-anchors.jsonl", "anchor JSONL path")
	keyPath := flags.String("public-key", "", "Ed25519 public key file (raw or base64)")
	keysDir := flags.String("public-keys-dir", "", "optional directory containing <key-id>.pub for rotated keys")
	_ = flags.Parse(args)
	if *keyPath == "" && *keysDir == "" {
		fatal("-public-key or -public-keys-dir is required")
	}
	var defaultKey []byte
	var err error
	if *keyPath != "" {
		defaultKey, err = readKey(*keyPath, ed25519.PublicKeySize)
		if err != nil {
			fatal("invalid public key: " + err.Error())
		}
	}
	f, err := os.Open(*anchorsPath)
	if err != nil {
		fatal(err.Error())
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	var previous uint64
	var previousHead string
	count := 0
	for scanner.Scan() {
		var record anchor
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			fatal(err.Error())
		}
		if record.Sequence < previous || (record.Sequence == previous && record.Head != previousHead) || len(record.Head) != 64 || record.KeyID == "" {
			fatal("anchor sequence or metadata invalid")
		}
		if _, err := hex.DecodeString(record.Head); err != nil {
			fatal("invalid evidence head")
		}
		key := defaultKey
		if *keysDir != "" {
			keyID := filepath.Base(record.KeyID)
			if keyID != record.KeyID || keyID == "." {
				fatal("unsafe anchor key ID")
			}
			key, err = readKey(filepath.Join(*keysDir, keyID+".pub"), ed25519.PublicKeySize)
			if err != nil {
				fatal("cannot load public key for " + keyID + ": " + err.Error())
			}
		}
		signature, err := base64.StdEncoding.DecodeString(record.Signature)
		if err != nil {
			fatal("invalid anchor signature encoding")
		}
		payload, _ := json.Marshal(record.statement)
		if !ed25519.Verify(ed25519.PublicKey(key), payload, signature) {
			fatal(fmt.Sprintf("signature invalid at anchor sequence %d", record.Sequence))
		}
		previous, previousHead = record.Sequence, record.Head
		count++
	}
	if err := scanner.Err(); err != nil {
		fatal(err.Error())
	}
	if count == 0 {
		fatal("anchor file is empty")
	}
	fmt.Printf("verified %d evidence anchors through sequence %d\n", count, previous)
}

func readKey(path string, size int) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(raw))
	if decoded, err := base64.StdEncoding.DecodeString(trimmed); err == nil && len(decoded) == size {
		return decoded, nil
	}
	if len(raw) == size {
		return raw, nil
	}
	return nil, errors.New("key must contain expected raw or base64-encoded key bytes")
}

func fatal(message string) { _, _ = io.WriteString(os.Stderr, message+"\n"); os.Exit(1) }
