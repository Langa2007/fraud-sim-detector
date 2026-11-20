package telco

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestSimulator(t *testing.T) {
	tmp, err := os.CreateTemp("", "telco-sim-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())

	data := map[string]int{
		"+254712345678": 38,
		"+254799999999": 1440,
	}
	raw, _ := json.Marshal(data)
	if _, err := tmp.Write(raw); err != nil {
		t.Fatal(err)
	}
	tmp.Close()

	sim, err := NewSimulator(tmp.Name())
	if err != nil {
		t.Fatalf("NewSimulator failed: %v", err)
	}

	sig, err := sim.Lookup(context.Background(), "+254712345678")
	if err != nil {
		t.Fatalf("Lookup error: %v", err)
	}
	if !sig.Available || sig.SIMSwapAgeMinutes == nil || *sig.SIMSwapAgeMinutes != 38 {
		t.Errorf("Unexpected signal for swapped number: %+v", sig)
	}

	sigUnknown, err := sim.Lookup(context.Background(), "+254700000000")
	if err != nil {
		t.Fatalf("Lookup error: %v", err)
	}
	if sigUnknown.Available {
		t.Errorf("Expected unavailable for unknown number, got %+v", sigUnknown)
	}
}

func TestCAMARAConnector(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sim-swap/v0/retrieve-date" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-camara-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req camaraDateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		if req.PhoneNumber == "+254711111111" {
			swappedAt := time.Now().Add(-45 * time.Minute)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(camaraDateResponse{LatestSimChange: &swappedAt})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	conn := NewCAMARAConnector(ts.URL, "test-camara-token", 500*time.Millisecond)

	sig, err := conn.Lookup(context.Background(), "+254711111111")
	if err != nil {
		t.Fatalf("CAMARA Lookup error: %v", err)
	}
	if !sig.Available || sig.SIMSwapAgeMinutes == nil {
		t.Fatalf("Expected available signal, got %+v", sig)
	}
	if *sig.SIMSwapAgeMinutes < 44 || *sig.SIMSwapAgeMinutes > 46 {
		t.Errorf("Expected ~45 minutes, got %d", *sig.SIMSwapAgeMinutes)
	}

	sigNotFound, err := conn.Lookup(context.Background(), "+254700000000")
	if err != nil {
		t.Fatalf("Expected nil err for 404, got %v", err)
	}
	if sigNotFound.Available {
		t.Errorf("Expected available=false for 404, got %+v", sigNotFound)
	}
}
