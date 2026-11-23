package config

import (
	"os"
	"strconv"
)

type Config struct {
	ListenAddr, APIKeys, EvidencePath, OutcomePath, CasePath, IdempotencyTTL, EngineURL, TelcoSimulatorPath, RedisURL string
	ConsortiumPath, ConsortiumAdminToken, TokenizerURL, TokenizerCallerKey, GraphFeaturesPath                         string
	CAMARAEndpoint, CAMARAToken, AirtelCAMARAEndpoint, AirtelCAMARAToken, TelkomCAMARAEndpoint, TelkomCAMARAToken     string
	ConsortiumMinConfirmations                                                                                        int
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func envInt(k string, d int) int {
	value, err := strconv.Atoi(os.Getenv(k))
	if err != nil || value < 2 {
		return d
	}
	return value
}
func Load() Config {
	return Config{
		ListenAddr:                 env("KINGA_LISTEN_ADDR", ":8080"),
		APIKeys:                    os.Getenv("KINGA_API_KEYS"),
		EvidencePath:               env("KINGA_EVIDENCE_PATH", "data/evidence.jsonl"),
		OutcomePath:                env("KINGA_OUTCOME_PATH", "data/outcomes.jsonl"),
		CasePath:                   env("KINGA_CASE_PATH", "data/cases.jsonl"),
		ConsortiumPath:             env("KINGA_CONSORTIUM_PATH", "data/consortium.jsonl"),
		ConsortiumAdminToken:       os.Getenv("KINGA_CONSORTIUM_ADMIN_TOKEN"),
		ConsortiumMinConfirmations: envInt("KINGA_CONSORTIUM_MIN_CONFIRMATIONS", 2),
		TokenizerURL:               env("KINGA_TOKENIZER_URL", "http://127.0.0.1:8091"),
		TokenizerCallerKey:         os.Getenv("KINGA_TOKENIZER_CALLER_KEY"),
		GraphFeaturesPath:          os.Getenv("KINGA_GRAPH_FEATURES_PATH"),
		CAMARAEndpoint:             os.Getenv("KINGA_CAMARA_ENDPOINT"),
		CAMARAToken:                os.Getenv("KINGA_CAMARA_TOKEN"),
		AirtelCAMARAEndpoint:       os.Getenv("KINGA_AIRTEL_CAMARA_ENDPOINT"),
		AirtelCAMARAToken:          os.Getenv("KINGA_AIRTEL_CAMARA_TOKEN"),
		TelkomCAMARAEndpoint:       os.Getenv("KINGA_TELKOM_CAMARA_ENDPOINT"),
		TelkomCAMARAToken:          os.Getenv("KINGA_TELKOM_CAMARA_TOKEN"),
		IdempotencyTTL:             env("KINGA_IDEMPOTENCY_TTL", "24h"),
		RedisURL:                   os.Getenv("KINGA_REDIS_URL"),
		EngineURL:                  env("KINGA_ENGINE_URL", "http://127.0.0.1:8090"),
		TelcoSimulatorPath:         os.Getenv("KINGA_TELCO_SIMULATOR_PATH"),
	}
}
