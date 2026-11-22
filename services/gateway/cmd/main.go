package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"kinga/services/gateway/internal/api"
	"kinga/services/gateway/internal/auth"
	"kinga/services/gateway/internal/cases"
	"kinga/services/gateway/internal/config"
	"kinga/services/gateway/internal/consortium"
	"kinga/services/gateway/internal/engine"
	"kinga/services/gateway/internal/evidence"
	"kinga/services/gateway/internal/features"
	"kinga/services/gateway/internal/graph"
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
	consortiumStore, err := consortium.Open(cfg.ConsortiumPath, cfg.ConsortiumMinConfirmations)
	if err != nil {
		log.Fatal(err)
	}
	defer consortiumStore.Close()
	keys := auth.ParseKeys(cfg.APIKeys)
	var telcoProviders []telco.Provider
	if cfg.CAMARAEndpoint != "" {
		telcoProviders = append(telcoProviders, telco.Provider{Name: "camara", Connector: telco.NewCAMARAConnector(cfg.CAMARAEndpoint, cfg.CAMARAToken, 80*time.Millisecond)})
	}
	if cfg.AirtelCAMARAEndpoint != "" {
		telcoProviders = append(telcoProviders, telco.Provider{Name: "airtel_camara", Connector: telco.NewCAMARAConnector(cfg.AirtelCAMARAEndpoint, cfg.AirtelCAMARAToken, 80*time.Millisecond)})
	}
	if cfg.TelkomCAMARAEndpoint != "" {
		telcoProviders = append(telcoProviders, telco.Provider{Name: "telkom_camara", Connector: telco.NewCAMARAConnector(cfg.TelkomCAMARAEndpoint, cfg.TelkomCAMARAToken, 80*time.Millisecond)})
	}
	if len(telcoProviders) == 0 {
		telcoSimulator, err := telco.NewSimulator(cfg.TelcoSimulatorPath)
		if err != nil {
			log.Fatal(err)
		}
		telcoProviders = append(telcoProviders, telco.Provider{Name: "simulator", Connector: telcoSimulator})
	}
	telcoConnector, err := telco.NewFailover(telcoProviders...)
	if err != nil {
		log.Fatal(err)
	}
	graphRuntime, err := graph.New(cfg.GraphFeaturesPath)
	if err != nil {
		log.Fatalf("load graph snapshot: %v", err)
	}
	if len(keys) == 0 {
		log.Print("WARNING: no KINGA_API_KEYS configured; assessments will be unauthorized")
	}
	h := api.New(store.New(cfg.IdempotencyTTL), engine.NewClient(cfg.EngineURL, 120*time.Millisecond), logStore, caseStore, outcomeStore, consortiumStore, keys, cfg, features.NewRuntime(), telcoConnector)
	h.SetGraphRuntime(graphRuntime)
	s := &http.Server{Addr: cfg.ListenAddr, Handler: h, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("Kinga shadow gateway listening on %s", cfg.ListenAddr)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- s.ListenAndServe() }()
	select {
	case err := <-serverErrors:
		if err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown timed out: %v", err)
			_ = s.Close()
		}
		if err := <-serverErrors; err != nil && err != http.ErrServerClosed {
			log.Printf("server stopped with error: %v", err)
		}
	}
}
