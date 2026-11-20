package telco

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

type Signal struct {
	SIMSwapAgeMinutes *int   `json:"sim_swap_age_minutes,omitempty"`
	Available         bool   `json:"available"`
	Source            string `json:"source"`
}
type Connector interface {
	Lookup(context.Context, string) (Signal, error)
}
type Simulator struct{ swaps map[string]int }

func NewSimulator(path string) (*Simulator, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var swaps map[string]int
	if err = json.Unmarshal(raw, &swaps); err != nil {
		return nil, err
	}
	for msisdn, age := range swaps {
		if len(msisdn) < 9 || msisdn[0] != '+' || age < 0 {
			return nil, fmt.Errorf("invalid simulator entry for %q", msisdn)
		}
	}
	return &Simulator{swaps}, nil
}
func (s *Simulator) Lookup(ctx context.Context, msisdn string) (Signal, error) {
	select {
	case <-ctx.Done():
		return Signal{}, ctx.Err()
	default:
	}
	age, ok := s.swaps[msisdn]
	if !ok {
		return Signal{Available: false, Source: "simulator"}, nil
	}
	return Signal{SIMSwapAgeMinutes: &age, Available: true, Source: "simulator"}, nil
}
