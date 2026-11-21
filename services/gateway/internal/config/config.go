package config

import "os"

type Config struct {
	ListenAddr, APIKeys, EvidencePath, OutcomePath, CasePath, IdempotencyTTL, EngineURL, TelcoSimulatorPath string
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func Load() Config {
	return Config{
		ListenAddr:         env("KINGA_LISTEN_ADDR", ":8080"),
		APIKeys:            os.Getenv("KINGA_API_KEYS"),
		EvidencePath:       env("KINGA_EVIDENCE_PATH", "data/evidence.jsonl"),
		OutcomePath:        env("KINGA_OUTCOME_PATH", "data/outcomes.jsonl"),
		CasePath:           env("KINGA_CASE_PATH", "data/cases.jsonl"),
		IdempotencyTTL:     env("KINGA_IDEMPOTENCY_TTL", "24h"),
		EngineURL:          env("KINGA_ENGINE_URL", "http://127.0.0.1:8090"),
		TelcoSimulatorPath: env("KINGA_TELCO_SIMULATOR_PATH", "testdata/telco/simulator.json"),
	}
}
