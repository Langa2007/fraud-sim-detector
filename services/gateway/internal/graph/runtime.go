// Package graph loads an immutable, token-keyed batch feature snapshot.
package graph

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"sync/atomic"
)

var tokenPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Feature struct {
	Namespace          string  `json:"namespace"`
	Token              string  `json:"token"`
	WindowDays         int     `json:"window_days"`
	Degree             int     `json:"degree"`
	InboundTransfers   int     `json:"inbound_transfers"`
	OutboundTransfers  int     `json:"outbound_transfers"`
	IndependentTenants int     `json:"independent_tenants"`
	MuleLikelihood     float64 `json:"mule_likelihood"`
	CommunityToken     string  `json:"community_token"`
	Version            string  `json:"version"`
}

type snapshot struct{ rows map[string]Feature }
type Runtime struct{ current atomic.Pointer[snapshot] }

func New(path string) (*Runtime, error) {
	r := &Runtime{}
	if path != "" {
		if err := r.Reload(path); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	return r, nil
}

func (r *Runtime) Reload(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	next := &snapshot{rows: map[string]Feature{}}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		var row Feature
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			return err
		}
		if !validNamespace(row.Namespace) || !tokenPattern.MatchString(row.Token) || row.WindowDays < 1 || row.IndependentTenants < 0 || row.MuleLikelihood < 0 || row.MuleLikelihood > 1 || row.Version != "kinga.graph.v1" {
			return errors.New("invalid graph feature row")
		}
		key := row.Namespace + "\x00" + row.Token
		if _, exists := next.rows[key]; exists {
			return errors.New("duplicate graph feature key")
		}
		next.rows[key] = row
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	r.current.Store(next)
	return nil
}

func (r *Runtime) Lookup(namespace, token string, minIndependent int) (Feature, bool) {
	if !validNamespace(namespace) || !tokenPattern.MatchString(token) {
		return Feature{}, false
	}
	state := r.current.Load()
	if state == nil {
		return Feature{}, false
	}
	row, ok := state.rows[namespace+"\x00"+token]
	if !ok || row.IndependentTenants < minIndependent {
		return Feature{}, false
	}
	return row, true
}
func validNamespace(v string) bool {
	return v == "msisdn" || v == "account" || v == "device" || v == "beneficiary" || v == "sim" || v == "agent" || v == "wallet"
}
