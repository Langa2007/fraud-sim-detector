package config

import "os"

type Config struct{ ListenAddr, APIKeys, EvidencePath, IdempotencyTTL, EngineURL, TelcoSimulatorPath string }

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func Load() Config {
	return Config{env("KINGA_LISTEN_ADDR", ":8080"), os.Getenv("KINGA_API_KEYS"), env("KINGA_EVIDENCE_PATH", "data/evidence.jsonl"), env("KINGA_IDEMPOTENCY_TTL", "24h"), env("KINGA_ENGINE_URL", "http://127.0.0.1:8090"), env("KINGA_TELCO_SIMULATOR_PATH", "testdata/telco/simulator.json")}
}
