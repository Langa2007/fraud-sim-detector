// stub-scorer implements the internal decision-engine HTTP contract for local
// gateway latency experiments. It must never be used for production decisions.
package main

import (
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

type request struct {
	Event struct {
		TenantEventID string `json:"tenant_event_id"`
	} `json:"event"`
}

func main() {
	addr := flag.String("addr", ":8092", "listen address")
	delay := flag.Duration("delay", 0, "optional deterministic scoring delay")
	healthcheck := flag.Bool("healthcheck", false, "check the local health endpoint and exit")
	flag.Parse()
	if *healthcheck {
		response, err := http.Get("http://127.0.0.1:8092/healthz")
		if err != nil {
			os.Exit(1)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	})
	mux.HandleFunc("POST /internal/v1/assess", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var input request
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&input); err != nil || input.Event.TenantEventID == "" {
			http.Error(w, `{"error":"invalid_event"}`, http.StatusBadRequest)
			return
		}
		if *delay > 0 {
			select {
			case <-time.After(*delay):
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"decision": "ALLOW", "mode": "shadow", "risk_score": 0.05,
			"risk_band": "low", "reasons": []any{}, "recommended_actions": []string{},
			"degraded": false, "tenant_event_id": input.Event.TenantEventID,
			"policy_version": "latency-spike", "model_version": nil, "latency_ms": 0,
		})
	})
	server := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 2 * time.Second}
	log.Printf("WARNING: latency spike stub scorer only; listening on %s", *addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("stub scorer failed: %v", err)
		os.Exit(1)
	}
}
