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

// Provider identifies a configured network adapter without placing provider
// credentials or subscriber data in logs.
type Provider struct {
	Name      string
	Connector Connector
}

// Failover tries providers in configured priority order and stops at the first
// valid signal. It never sends requests to providers concurrently.
type Failover struct{ providers []Provider }

func NewFailover(providers ...Provider) (*Failover, error) {
	seen := map[string]bool{}
	for _, provider := range providers {
		if provider.Name == "" || provider.Connector == nil || seen[provider.Name] {
			return nil, fmt.Errorf("invalid or duplicate telco provider")
		}
		seen[provider.Name] = true
	}
	if len(providers) == 0 {
		return nil, fmt.Errorf("at least one telco provider is required")
	}
	return &Failover{providers: append([]Provider(nil), providers...)}, nil
}

func (f *Failover) Lookup(ctx context.Context, msisdn string) (Signal, error) {
	var failures []string
	for _, provider := range f.providers {
		if err := ctx.Err(); err != nil {
			return Signal{Available: false, Source: "provider_chain"}, err
		}
		signal, err := provider.Connector.Lookup(ctx, msisdn)
		if err == nil && signal.Available {
			signal.Source = provider.Name
			return signal, nil
		}
		if err != nil {
			failures = append(failures, provider.Name+": unavailable")
		}
	}
	if len(failures) > 0 {
		return Signal{Available: false, Source: "provider_chain"}, fmt.Errorf("all configured telco providers unavailable (%s)", strings.Join(failures, ", "))
	}
	return Signal{Available: false, Source: "provider_chain"}, nil
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
