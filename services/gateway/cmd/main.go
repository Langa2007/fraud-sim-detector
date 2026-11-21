package main

import (
	"log"
	"net/http"
	"time"

	"kinga/services/gateway/internal/api"
	"kinga/services/gateway/internal/auth"
	"kinga/services/gateway/internal/cases"
	"kinga/services/gateway/internal/config"
	"kinga/services/gateway/internal/engine"
	"kinga/services/gateway/internal/evidence"
	"kinga/services/gateway/internal/features"
	"kinga/services/gateway/internal/outcomes"
	"kinga/services/gateway/internal/store"
	"kinga/services/gateway/internal/telco"
)

func main() {
	cfg := config.Load()
	logStore, err := evidence.Open(cfg.EvidencePath)
	if err != nil {
		log.Fatal(err)
	}
	defer logStore.Close()
	outcomeStore, err := outcomes.Open(cfg.OutcomePath)
	if err != nil {
		log.Fatal(err)
	}
	defer outcomeStore.Close()
	caseStore, err := cases.Open(cfg.CasePath)
	if err != nil {
		log.Fatal(err)
	}
	defer caseStore.Close()
	keys := auth.ParseKeys(cfg.APIKeys)
	telcoSimulator, err := telco.NewSimulator(cfg.TelcoSimulatorPath)
	if err != nil {
		log.Fatal(err)
	}
	if len(keys) == 0 {
		log.Print("WARNING: no KINGA_API_KEYS configured; assessments will be unauthorized")
	}
	h := api.New(store.New(cfg.IdempotencyTTL), engine.NewClient(cfg.EngineURL, 120*time.Millisecond), logStore, caseStore, outcomeStore, keys, cfg, features.NewRuntime(), telcoSimulator)
	s := &http.Server{Addr: cfg.ListenAddr, Handler: h, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("Kinga shadow gateway listening on %s", cfg.ListenAddr)
	if err := s.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
