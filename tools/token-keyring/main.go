// token-keyring creates, rotates, and retires Rust tokenizer keyring files.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type keyring struct {
	Active   string            `json:"active"`
	Previous []string          `json:"previous"`
	Keys     map[string]string `json:"keys"`
}

func main() {
	if len(os.Args) < 2 {
		fatal("usage: token-keyring <create|rotate|retire> [flags]")
	}
	switch os.Args[1] {
	case "create":
		create(os.Args[2:])
	case "rotate":
		rotate(os.Args[2:])
	case "retire":
		retire(os.Args[2:])
	default:
		fatal("expected create, rotate, or retire")
	}
}

func create(args []string) {
	flags := flag.NewFlagSet("create", flag.ExitOnError)
	version := flags.String("version", "", "new active version ID")
	output := flags.String("output", "", "new keyring file")
	_ = flags.Parse(args)
	require(*version, *output)
	material, err := newSecret()
	if err != nil {
		fatal(err.Error())
	}
	save(*output, keyring{Active: *version, Previous: []string{}, Keys: map[string]string{*version: material}})
}

func rotate(args []string) {
	flags := flag.NewFlagSet("rotate", flag.ExitOnError)
	input := flags.String("input", "", "current keyring file")
	version := flags.String("version", "", "new active version ID")
	output := flags.String("output", "", "new keyring file")
	_ = flags.Parse(args)
	require(*input, *version, *output)
	old := load(*input)
	if err := validate(old); err != nil {
		fatal("invalid input keyring: " + err.Error())
	}
	if _, exists := old.Keys[*version]; exists {
		fatal("new version already exists")
	}
	material, err := newSecret()
	if err != nil {
		fatal(err.Error())
	}
	previous := append([]string{old.Active}, old.Previous...)
	keys := make(map[string]string, len(old.Keys)+1)
	for k, v := range old.Keys {
		keys[k] = v
	}
	keys[*version] = material
	save(*output, keyring{Active: *version, Previous: previous, Keys: keys})
}

func retire(args []string) {
	flags := flag.NewFlagSet("retire", flag.ExitOnError)
	input := flags.String("input", "", "current keyring file")
	keepRaw := flags.String("keep", "", "comma-separated version IDs to retain")
	output := flags.String("output", "", "new keyring file")
	_ = flags.Parse(args)
	require(*input, *keepRaw, *output)
	old := load(*input)
	if err := validate(old); err != nil {
		fatal("invalid input keyring: " + err.Error())
	}
	keep := strings.Split(*keepRaw, ",")
	retained := map[string]bool{}
	for _, version := range keep {
		version = strings.TrimSpace(version)
		if !validVersion(version) || retained[version] {
			fatal("invalid or duplicate version in -keep")
		}
		if _, ok := old.Keys[version]; !ok {
			fatal("requested key version is not present")
		}
		retained[version] = true
	}
	if !retained[old.Active] {
		fatal("-keep must include the active version")
	}
	previous := make([]string, 0, len(keep)-1)
	keys := make(map[string]string, len(keep))
	for _, version := range keep {
		version = strings.TrimSpace(version)
		keys[version] = old.Keys[version]
		if version != old.Active {
			previous = append(previous, version)
		}
	}
	save(*output, keyring{Active: old.Active, Previous: previous, Keys: keys})
}

func newSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
func load(path string) keyring {
	raw, err := os.ReadFile(path)
	if err != nil {
		fatal("read keyring: " + err.Error())
	}
	var ring keyring
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoderErr := decoder.Decode(&ring); decoderErr != nil {
		fatal("decode keyring: " + decoderErr.Error())
	}
	if decoderErr := decoder.Decode(&struct{}{}); decoderErr != io.EOF {
		fatal("keyring must contain exactly one JSON object")
	}
	return ring
}
func validate(r keyring) error {
	if !validVersion(r.Active) || len(r.Keys) == 0 {
		return errors.New("missing or invalid active version")
	}
	versions := map[string]bool{r.Active: true}
	for _, v := range r.Previous {
		if !validVersion(v) || versions[v] {
			return errors.New("invalid or duplicate previous version")
		}
		versions[v] = true
	}
	if len(versions) != len(r.Keys) {
		return errors.New("key map must contain only active and previous versions")
	}
	materials := map[string]bool{}
	for version := range versions {
		encoded, ok := r.Keys[version]
		if !ok {
			return errors.New("version has no key material")
		}
		raw, err := hex.DecodeString(encoded)
		if err != nil || len(raw) != 32 {
			return errors.New("each version must contain a 32-byte hexadecimal key")
		}
		if materials[string(raw)] {
			return errors.New("key material must be unique per version")
		}
		materials[string(raw)] = true
	}
	return nil
}
func save(path string, ring keyring) {
	if !validVersion(ring.Active) {
		fatal("invalid key version; use 1-64 ASCII letters, digits, underscore or hyphen")
	}
	if err := validate(ring); err != nil {
		fatal(err.Error())
	}
	raw, err := json.MarshalIndent(ring, "", "  ")
	if err != nil {
		fatal(err.Error())
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		fatal(err.Error())
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		fatal("create output without overwriting: " + err.Error())
	}
	if _, err = file.Write(append(raw, '\n')); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		fatal(err.Error())
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		fatal(err.Error())
	}
	if err = file.Close(); err != nil {
		_ = os.Remove(path)
		fatal(err.Error())
	}
	fmt.Printf("wrote keyring: active=%s previous=%d; protect this file as a secret\n", ring.Active, len(ring.Previous))
}
func validVersion(v string) bool {
	if v == "" || len(v) > 64 {
		return false
	}
	for _, c := range v {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
func require(values ...string) {
	for _, value := range values {
		if value == "" {
			fatal("required flag is missing")
		}
	}
}
func fatal(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(1) }
