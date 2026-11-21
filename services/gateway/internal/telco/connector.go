package telco

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

type Signal struct {
	SIMSwapAgeMinutes *int   `json:"sim_swap_age_minutes,omitempty"`
	Available         bool   `json:"available"`
	Source            string `json:"source"`
}

type Connector interface {
	Lookup(context.Context, string) (Signal, error)
}

type Simulator struct {
	swaps map[string]int
}

func NewSimulator(path string) (*Simulator, error) {
	if path == "" {
		return &Simulator{swaps: map[string]int{}}, nil
	}
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
	if s == nil || s.swaps == nil {
		return Signal{Available: false, Source: "simulator"}, nil
	}
	age, ok := s.swaps[msisdn]
	if !ok {
		return Signal{Available: false, Source: "simulator"}, nil
	}
	return Signal{SIMSwapAgeMinutes: &age, Available: true, Source: "simulator"}, nil
}

// CAMARAConnector implements the GSMA Open Gateway / CAMARA SIM Swap API (v0.4.0)
// Calling /sim-swap/v0/retrieve-date to obtain the timestamp of the last SIM change.
type CAMARAConnector struct {
	endpoint string
	token    string
	client   *http.Client
}

type camaraDateRequest struct {
	PhoneNumber string `json:"phoneNumber"`
}

type camaraDateResponse struct {
	LatestSimChange *time.Time `json:"latestSimChange"`
}

func NewCAMARAConnector(endpoint, token string, timeout time.Duration) *CAMARAConnector {
	return &CAMARAConnector{
		endpoint: strings.TrimRight(endpoint, "/"),
		token:    token,
		client:   &http.Client{Timeout: timeout},
	}
}

func (c *CAMARAConnector) Lookup(ctx context.Context, msisdn string) (Signal, error) {
	payload, err := json.Marshal(camaraDateRequest{PhoneNumber: msisdn})
	if err != nil {
		return Signal{}, err
	}

	url := c.endpoint + "/sim-swap/v0/retrieve-date"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return Signal{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return Signal{Available: false, Source: "camara"}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return Signal{Available: false, Source: "camara"}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return Signal{Available: false, Source: "camara"}, fmt.Errorf("camara api status %d", resp.StatusCode)
	}

	var res camaraDateResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return Signal{Available: false, Source: "camara"}, err
	}

	if res.LatestSimChange == nil {
		return Signal{Available: true, Source: "camara"}, nil
	}

	ageMinutes := int(time.Since(*res.LatestSimChange).Minutes())
	if ageMinutes < 0 {
		ageMinutes = 0
	}
	return Signal{
		SIMSwapAgeMinutes: &ageMinutes,
		Available:         true,
		Source:            "camara",
	}, nil
}
