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
	"kinga/services/gateway/internal/consortium"
	"kinga/services/gateway/internal/engine"
	"kinga/services/gateway/internal/evidence"
	"kinga/services/gateway/internal/features"
	"kinga/services/gateway/internal/graph"
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
	h := New(store.New("1h"), client, evLog, caseLog, outcomeLog, nil, keys, cfg, features.NewRuntime(), sim)

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

func TestAPIConsortiumAndGraph(t *testing.T) {
	var observedEvent map[string]any
	engineSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/v1/assess" {
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			observedEvent = req["event"].(map[string]any)

			resp := engine.Result{
				Decision:      "HOLD",
				Mode:          "shadow",
				RiskScore:     0.9,
				RiskBand:      "critical",
				TenantEventID: "evt_p3_1",
				PolicyVersion: "2026-10-28.3",
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer engineSrv.Close()

	dir := t.TempDir()
	evLog, _ := evidence.Open(filepath.Join(dir, "evidence.jsonl"))
	defer evLog.Close()
	outcomeLog, _ := outcomes.Open(filepath.Join(dir, "outcomes.jsonl"))
	defer outcomeLog.Close()
	caseLog, _ := cases.Open(filepath.Join(dir, "cases.jsonl"))
	defer caseLog.Close()

	coStore, err := consortium.Open(filepath.Join(dir, "consortium.jsonl"), 2)
	if err != nil {
		t.Fatalf("open consortium: %v", err)
	}
	defer coStore.Close()

	token := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	graphPath := filepath.Join(dir, "graph.jsonl")
	graphLine := `{"namespace":"wallet","token":"` + token + `","window_days":30,"degree":5,"inbound_transfers":10,"outbound_transfers":8,"independent_tenants":3,"mule_likelihood":0.82,"community_token":"comm1","version":"kinga.graph.v1"}` + "\n"
	_ = os.WriteFile(graphPath, []byte(graphLine), 0600)
	gRuntime, err := graph.New(graphPath)
	if err != nil {
		t.Fatalf("open graph: %v", err)
	}

	keys := map[string]auth.Credential{
		"key-bank-a": {Tenant: "bank_a", Secret: []byte("secret-a")},
		"key-bank-b": {Tenant: "bank_b", Secret: []byte("secret-b")},
	}
	cfg := config.Config{
		EvidencePath:              filepath.Join(dir, "evidence.jsonl"),
		IdempotencyTTL:            "1h",
		EngineURL:                 engineSrv.URL,
		ConsortiumMinConfirmations: 2,
	}

	sim, _ := telco.NewSimulator("")
	client := engine.NewClient(engineSrv.URL, 500*time.Millisecond)
	h := New(store.New("1h"), client, evLog, caseLog, outcomeLog, coStore, keys, cfg, features.NewRuntime(), sim)
	h.SetGraphRuntime(gRuntime)

	ts := strconv.FormatInt(time.Now().Unix(), 10)

	// 1. Contribute from bank_a
	contribPayload := `{"namespace":"beneficiary","token":"` + token + `","confidence":0.85,"ttl_hours":48}`
	req1 := httptest.NewRequest("POST", "/v1/consortium/indicators", bytes.NewReader([]byte(contribPayload)))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("X-Kinga-Key", "key-bank-a")
	req1.Header.Set("X-Kinga-Timestamp", ts)
	req1.Header.Set("X-Kinga-Signature", auth.Sign([]byte("secret-a"), ts, contribPayload))
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, req1)
	if rec1.Code != 201 {
		t.Fatalf("bank_a contribute failed: %d: %s", rec1.Code, rec1.Body.String())
	}

	// 2. Query indicator before privacy floor is met (only 1 contributor) -> 404
	reqQ1 := httptest.NewRequest("GET", "/v1/consortium/indicators/beneficiary/"+token, nil)
	reqQ1.Header.Set("X-Kinga-Key", "key-bank-a")
	reqQ1.Header.Set("X-Kinga-Timestamp", ts)
	reqQ1.Header.Set("X-Kinga-Signature", auth.Sign([]byte("secret-a"), ts, ""))
	recQ1 := httptest.NewRecorder()
	h.ServeHTTP(recQ1, reqQ1)
	if recQ1.Code != 404 {
		t.Fatalf("expected 404 below privacy threshold, got %d", recQ1.Code)
	}

	// 3. Contribute from bank_b
	req2 := httptest.NewRequest("POST", "/v1/consortium/indicators", bytes.NewReader([]byte(contribPayload)))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-Kinga-Key", "key-bank-b")
	req2.Header.Set("X-Kinga-Timestamp", ts)
	req2.Header.Set("X-Kinga-Signature", auth.Sign([]byte("secret-b"), ts, contribPayload))
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 201 {
		t.Fatalf("bank_b contribute failed: %d: %s", rec2.Code, rec2.Body.String())
	}

	// 4. Query indicator now -> 200 with 2 confirmations
	reqQ2 := httptest.NewRequest("GET", "/v1/consortium/indicators/beneficiary/"+token, nil)
	reqQ2.Header.Set("X-Kinga-Key", "key-bank-a")
	reqQ2.Header.Set("X-Kinga-Timestamp", ts)
	reqQ2.Header.Set("X-Kinga-Signature", auth.Sign([]byte("secret-a"), ts, ""))
	recQ2 := httptest.NewRecorder()
	h.ServeHTTP(recQ2, reqQ2)
	if recQ2.Code != 200 {
		t.Fatalf("query indicator failed: %d: %s", recQ2.Code, recQ2.Body.String())
	}

	// 5. Query member contribution stats
	reqMem := httptest.NewRequest("GET", "/v1/consortium/members/me", nil)
	reqMem.Header.Set("X-Kinga-Key", "key-bank-a")
	reqMem.Header.Set("X-Kinga-Timestamp", ts)
	reqMem.Header.Set("X-Kinga-Signature", auth.Sign([]byte("secret-a"), ts, ""))
	recMem := httptest.NewRecorder()
	h.ServeHTTP(recMem, reqMem)
	if recMem.Code != 200 {
		t.Fatalf("consortium member stats failed: %d", recMem.Code)
	}

	// 6. Graph lookup
	reqGraph := httptest.NewRequest("GET", "/v1/graph/wallet/"+token, nil)
	reqGraph.Header.Set("X-Kinga-Key", "key-bank-a")
	reqGraph.Header.Set("X-Kinga-Timestamp", ts)
	reqGraph.Header.Set("X-Kinga-Signature", auth.Sign([]byte("secret-a"), ts, ""))
	recGraph := httptest.NewRecorder()
	h.ServeHTTP(recGraph, reqGraph)
	if recGraph.Code != 200 {
		t.Fatalf("graph lookup failed: %d: %s", recGraph.Code, recGraph.Body.String())
	}

	// 7. Online Assessment with Beneficiary Consortium Hit and Mule Risk
	assessPayload := `{
		"tenant_event_id": "evt_p3_1",
		"event_type": "funds_transfer",
		"occurred_at": "` + time.Now().UTC().Format(time.RFC3339) + `",
		"channel": "mobile_app",
		"subject": {"account_ref": "ACC_P3_TEST"},
		"transaction": {
			"amount": 25000,
			"currency": "KES",
			"beneficiary": {"type": "wallet", "ref": "` + token + `"}
		}
	}`
	reqAssess := httptest.NewRequest("POST", "/v1/assess", bytes.NewReader([]byte(assessPayload)))
	reqAssess.Header.Set("Content-Type", "application/json")
	reqAssess.Header.Set("Idempotency-Key", "evt_p3_1")
	reqAssess.Header.Set("X-Kinga-Key", "key-bank-a")
	reqAssess.Header.Set("X-Kinga-Timestamp", ts)
	reqAssess.Header.Set("X-Kinga-Signature", auth.Sign([]byte("secret-a"), ts, assessPayload))
	recAssess := httptest.NewRecorder()
	h.ServeHTTP(recAssess, reqAssess)
	if recAssess.Code != 200 {
		t.Fatalf("assess failed with status %d: %s", recAssess.Code, recAssess.Body.String())
	}

	// Verify signals reached the engine
	signals := observedEvent["signals"].(map[string]any)
	if hit, _ := signals["beneficiary_consortium_hit"].(bool); !hit {
		t.Errorf("expected beneficiary_consortium_hit to be true in assess signals")
	}
	if confs, _ := signals["consortium_confirmations"].(float64); int(confs) != 2 {
		t.Errorf("expected consortium_confirmations to be 2, got %v", confs)
	}
	if muleLike, ok := signals["graph_mule_likelihood"].(float64); !ok || muleLike != 0.82 {
		t.Errorf("expected graph_mule_likelihood to be 0.82, got %v", signals["graph_mule_likelihood"])
	}
}
