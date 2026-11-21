package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

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

func TestAPIEndToEnd(t *testing.T) {
	// 1. Mock engine server
	engineSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		if r.URL.Path == "/internal/v1/assess" {
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			ev := req["event"].(map[string]any)
			eventID := ev["tenant_event_id"].(string)

			resp := engine.Result{
				Decision:           "HOLD",
				Mode:               "shadow",
				RiskScore:          0.85,
				RiskBand:           "critical",
				TenantEventID:      eventID,
				PolicyVersion:      "2026-10-28.3",
				Reasons:            []engine.Reason{{Code: "SIM_SWAP_RECENT", Detail: "SIM replaced 38m ago", Weight: 0.6}},
				RecommendedActions: []string{"ALERT_FRAUD_TEAM", "RECOMMEND_HOLD"},
				Degraded:           false,
				LatencyMS:          12,
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer engineSrv.Close()

	// 2. Setup dependencies
	dir := t.TempDir()
	evPath := filepath.Join(dir, "evidence.jsonl")
	evLog, err := evidence.Open(evPath)
	if err != nil {
		t.Fatal(err)
	}
	defer evLog.Close()
	outcomeLog, err := outcomes.Open(filepath.Join(dir, "outcomes.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer outcomeLog.Close()
	caseLog, err := cases.Open(filepath.Join(dir, "cases.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer caseLog.Close()

	simPath := filepath.Join(dir, "simulator.json")
	_ = os.WriteFile(simPath, []byte(`{"+254712345678": 38}`), 0600)
	sim, err := telco.NewSimulator(simPath)
	if err != nil {
		t.Fatal(err)
	}

	keys := map[string]auth.Credential{
		"test-key": {Tenant: "demo", Secret: []byte("test-secret")},
	}
	cfg := config.Config{
		EvidencePath:       evPath,
		IdempotencyTTL:     "1h",
		EngineURL:          engineSrv.URL,
		TelcoSimulatorPath: simPath,
	}

	client := engine.NewClient(engineSrv.URL, 500*time.Millisecond)
	h := New(store.New("1h"), client, evLog, caseLog, outcomeLog, keys, cfg, features.NewRuntime(), sim)

	// 3. Test Health & Readyz
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != 200 {
		t.Errorf("healthz failed with status %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/readyz", nil))
	if rec.Code != 200 {
		t.Errorf("readyz failed with status %d", rec.Code)
	}

	// 4. Test Metrics
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if rec.Code != 200 {
		t.Errorf("metrics failed with status %d", rec.Code)
	}

	// 5. Test Assess
	payload := `{
		"tenant_event_id": "evt_test_001",
		"event_type": "funds_transfer",
		"occurred_at": "` + time.Now().UTC().Format(time.RFC3339) + `",
		"channel": "mobile_app",
		"subject": {
			"msisdn": "+254712345678",
			"account_ref": "acc_001"
		},
		"device": {
			"device_id": "dvc_999"
		},
		"transaction": {
			"amount": 75000,
			"currency": "KES",
			"rail": "pesalink",
			"beneficiary": {
				"type": "bank_account",
				"ref": "ben_888"
			}
		}
	}`

	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := auth.Sign(keys["test-key"].Secret, ts, payload)

	req := httptest.NewRequest("POST", "/v1/assess", bytes.NewReader([]byte(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Kinga-Key", "test-key")
	req.Header.Set("X-Kinga-Timestamp", ts)
	req.Header.Set("X-Kinga-Signature", sig)
	req.Header.Set("Idempotency-Key", "evt_test_001")

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("assess failed with status %d, body: %s", rec.Code, rec.Body.String())
	}

	var res map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res["decision"] != "HOLD" || res["mode"] != "shadow" {
		t.Errorf("unexpected assess result: %+v", res)
	}
	if res["evidence_id"] == nil || res["evidence_id"] == "" {
		t.Errorf("expected evidence_id to be populated")
	}

	// 6. Test Idempotent Replay
	reqReplay := httptest.NewRequest("POST", "/v1/assess", bytes.NewReader([]byte(payload)))
	reqReplay.Header.Set("Content-Type", "application/json")
	reqReplay.Header.Set("X-Kinga-Key", "test-key")
	reqReplay.Header.Set("X-Kinga-Timestamp", ts)
	reqReplay.Header.Set("X-Kinga-Signature", sig)
	reqReplay.Header.Set("Idempotency-Key", "evt_test_001")

	recReplay := httptest.NewRecorder()
	h.ServeHTTP(recReplay, reqReplay)
	if recReplay.Code != 200 {
		t.Fatalf("idempotent replay failed with status %d", recReplay.Code)
	}
	if recReplay.Header().Get("Idempotent-Replay") != "true" {
		t.Errorf("expected Idempotent-Replay header to be true")
	}

	// 7. Verify evidence endpoint
	recVerify := httptest.NewRecorder()
	h.ServeHTTP(recVerify, httptest.NewRequest("GET", "/v1/evidence/verify", nil))
	if recVerify.Code != 200 {
		t.Fatalf("evidence verify failed with status %d: %s", recVerify.Code, recVerify.Body.String())
	}

	// 8. Test Step-up Challenge lifecycle
	stepUpPayload := `{"tenant_event_id":"evt_test_001","channel":"ussd_push","language":"sw","amount":75000,"beneficiary":"ben_888","ttl_seconds":60}`
	stepUpTs := strconv.FormatInt(time.Now().Unix(), 10)
	stepUpSig := auth.Sign(keys["test-key"].Secret, stepUpTs, stepUpPayload)

	reqStepUp := httptest.NewRequest("POST", "/v1/stepup/challenges", bytes.NewReader([]byte(stepUpPayload)))
	reqStepUp.Header.Set("Content-Type", "application/json")
	reqStepUp.Header.Set("X-Kinga-Key", "test-key")
	reqStepUp.Header.Set("X-Kinga-Timestamp", stepUpTs)
	reqStepUp.Header.Set("X-Kinga-Signature", stepUpSig)

	recStepUp := httptest.NewRecorder()
	h.ServeHTTP(recStepUp, reqStepUp)
	if recStepUp.Code != 201 {
		t.Fatalf("stepup initiation failed with status %d: %s", recStepUp.Code, recStepUp.Body.String())
	}
	var chResp map[string]any
	_ = json.Unmarshal(recStepUp.Body.Bytes(), &chResp)
	chID := chResp["challenge_id"].(string)

	// Fetch step-up challenge
	reqGetCh := httptest.NewRequest("GET", "/v1/stepup/challenges/"+chID, nil)
	reqGetCh.Header.Set("X-Kinga-Key", "test-key")
	reqGetCh.Header.Set("X-Kinga-Timestamp", stepUpTs)
	reqGetCh.Header.Set("X-Kinga-Signature", auth.Sign(keys["test-key"].Secret, stepUpTs, ""))
	recGetCh := httptest.NewRecorder()
	h.ServeHTTP(recGetCh, reqGetCh)
	if recGetCh.Code != 200 {
		t.Fatalf("get stepup failed with status %d", recGetCh.Code)
	}

	// Respond to step-up challenge
	respPayload := `{"approved":true}`
	respSig := auth.Sign(keys["test-key"].Secret, stepUpTs, respPayload)
	reqRespCh := httptest.NewRequest("POST", "/v1/stepup/challenges/"+chID+"/respond", bytes.NewReader([]byte(respPayload)))
	reqRespCh.Header.Set("Content-Type", "application/json")
	reqRespCh.Header.Set("X-Kinga-Key", "test-key")
	reqRespCh.Header.Set("X-Kinga-Timestamp", stepUpTs)
	reqRespCh.Header.Set("X-Kinga-Signature", respSig)
	recRespCh := httptest.NewRecorder()
	h.ServeHTTP(recRespCh, reqRespCh)
	if recRespCh.Code != 200 {
		t.Fatalf("respond stepup failed with status %d", recRespCh.Code)
	}

	// 9. Test Rollout configuration
	rolloutPayload := `{"target_mode":"enforce","rollout_percentage":25,"kill_switch_active":false,"friction_ceiling":0.015,"friction_window":500}`
	rolloutSig := auth.Sign(keys["test-key"].Secret, stepUpTs, rolloutPayload)
	reqRollout := httptest.NewRequest("POST", "/v1/rollout", bytes.NewReader([]byte(rolloutPayload)))
	reqRollout.Header.Set("Content-Type", "application/json")
	reqRollout.Header.Set("X-Kinga-Key", "test-key")
	reqRollout.Header.Set("X-Kinga-Timestamp", stepUpTs)
	reqRollout.Header.Set("X-Kinga-Signature", rolloutSig)
	recRollout := httptest.NewRecorder()
	h.ServeHTTP(recRollout, reqRollout)
	if recRollout.Code != 200 {
		t.Fatalf("set rollout failed with status %d", recRollout.Code)
	}

	// 10. Test Case Evidence Pack HTML export
	casesList := caseLog.List("demo", "open", 10)
	if len(casesList) > 0 {
		caseID := casesList[0].CaseID
		reqPack := httptest.NewRequest("GET", "/v1/cases/"+caseID+"/evidence/pack", nil)
		reqPack.Header.Set("X-Kinga-Key", "test-key")
		reqPack.Header.Set("X-Kinga-Timestamp", stepUpTs)
		reqPack.Header.Set("X-Kinga-Signature", auth.Sign(keys["test-key"].Secret, stepUpTs, ""))
		recPack := httptest.NewRecorder()
		h.ServeHTTP(recPack, reqPack)
		if recPack.Code != 200 || !strings.Contains(recPack.Body.String(), "Kinga Fraud Evidence Pack") {
			t.Fatalf("evidence pack failed with status %d: %s", recPack.Code, recPack.Body.String())
		}
	}
}
