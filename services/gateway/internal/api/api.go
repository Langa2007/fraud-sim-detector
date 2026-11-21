package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
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

type API struct {
	idem             *store.Store
	engine           *engine.Client
	evidence         *evidence.Log
	cases            *cases.Store
	outcomes         *outcomes.Store
	features         *features.Runtime
	telco            telco.Connector
	keys             map[string]auth.Credential
	cfg              config.Config
	mux              *http.ServeMux
	rateMu           sync.Mutex
	rates            map[string]rateWindow
	keyMu            sync.Mutex
	keyLocks         map[string]*keyLock
	metricMu         sync.Mutex
	decisionCounters map[string]uint64
	assessCount      uint64
	degradedCount    uint64
	latencyTotalMS   uint64
}
type rateWindow struct {
	start time.Time
	count int
}
type keyLock struct {
	mu   sync.Mutex
	refs int
}

func New(s *store.Store, e *engine.Client, l *evidence.Log, cs *cases.Store, o *outcomes.Store, k map[string]auth.Credential, c config.Config, f *features.Runtime, t telco.Connector) *API {
	a := &API{idem: s, engine: e, evidence: l, cases: cs, outcomes: o, features: f, telco: t, keys: k, cfg: c, mux: http.NewServeMux(), rates: map[string]rateWindow{}, keyLocks: map[string]*keyLock{}, decisionCounters: map[string]uint64{}}
	a.mux.HandleFunc("GET /healthz", a.health)
	a.mux.HandleFunc("GET /readyz", a.ready)
	a.mux.HandleFunc("GET /metrics", a.metrics)
	a.mux.HandleFunc("POST /v1/assess", a.assess)
	a.mux.HandleFunc("GET /v1/evidence/verify", a.verify)
	a.mux.HandleFunc("POST /v1/outcomes", a.submitOutcome)
	a.mux.HandleFunc("GET /v1/cases", a.listCases)
	a.mux.HandleFunc("POST /v1/cases/{caseID}/actions", a.applyCaseAction)
	a.mux.HandleFunc("GET /v1/cases/{caseID}/evidence", a.exportCaseEvidence)
	return a
}
func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) { a.mux.ServeHTTP(w, r) }
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func signedCredential(r *http.Request, body []byte, keys map[string]auth.Credential) (auth.Credential, bool) {
	credential, ok := auth.Verify(r, body, keys)
	if !ok {
		return auth.Credential{}, false
	}
	timestamp, err := strconv.ParseInt(r.Header.Get("X-Kinga-Timestamp"), 10, 64)
	if err != nil || time.Since(time.Unix(timestamp, 0)) > 5*time.Minute || time.Until(time.Unix(timestamp, 0)) > 5*time.Minute {
		return auth.Credential{}, false
	}
	if r.Header.Get("X-Kinga-Tenant") != "" && r.Header.Get("X-Kinga-Tenant") != credential.Tenant {
		return auth.Credential{}, false
	}
	return credential, true
}
func (a *API) health(w http.ResponseWriter, _ *http.Request) {
	write(w, 200, map[string]string{"status": "ok", "mode": "shadow"})
}
func (a *API) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 100*time.Millisecond)
	defer cancel()
	if err := a.engine.Ready(ctx); err != nil {
		write(w, 503, map[string]string{"status": "not_ready", "dependency": "decision_engine"})
		return
	}
	write(w, 200, map[string]string{"status": "ready", "mode": "shadow"})
}
func (a *API) metrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	a.metricMu.Lock()
	defer a.metricMu.Unlock()
	io.WriteString(w, "# HELP kinga_build_info Kinga phase one service is running\n# TYPE kinga_build_info gauge\nkinga_build_info{mode=\"shadow\"} 1\n")
	io.WriteString(w, "# TYPE kinga_assessments_total counter\n")
	fmt.Fprintf(w, "kinga_assessments_total %d\n", a.assessCount)
	io.WriteString(w, "# TYPE kinga_assessment_degraded_total counter\n")
	fmt.Fprintf(w, "kinga_assessment_degraded_total %d\n", a.degradedCount)
	io.WriteString(w, "# TYPE kinga_assessment_latency_ms_sum counter\n")
	fmt.Fprintf(w, "kinga_assessment_latency_ms_sum %d\n", a.latencyTotalMS)
	io.WriteString(w, "# TYPE kinga_decisions_total counter\n")
	keys := make([]string, 0, len(a.decisionCounters))
	for k := range a.decisionCounters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(w, "kinga_decisions_total{decision=%q} %d\n", k, a.decisionCounters[k])
	}
}

var eventID = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

func (a *API) assess(w http.ResponseWriter, r *http.Request) {
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "application/json" {
		write(w, 415, map[string]string{"error": "content_type_must_be_application_json"})
		return
	}
	body, e := io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
	if e != nil || len(body) > 1<<20 {
		write(w, 413, map[string]string{"error": "request_too_large"})
		return
	}
	cred, ok := auth.Verify(r, body, a.keys)
	if !ok {
		write(w, 401, map[string]string{"error": "invalid_api_credentials_or_signature"})
		return
	}
	date, err := strconv.ParseInt(r.Header.Get("X-Kinga-Timestamp"), 10, 64)
	if err != nil || time.Since(time.Unix(date, 0)) > 5*time.Minute || time.Until(time.Unix(date, 0)) > 5*time.Minute {
		write(w, 401, map[string]string{"error": "signature_timestamp_outside_window"})
		return
	}
	var ev engine.Event
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if e = d.Decode(&ev); e != nil {
		write(w, 400, map[string]string{"error": "invalid_event", "detail": e.Error()})
		return
	}
	var extra any
	if e = d.Decode(&extra); !errors.Is(e, io.EOF) {
		write(w, 400, map[string]string{"error": "request_must_contain_one_json_object"})
		return
	}
	if ev.TenantEventID == "" || !eventID.MatchString(ev.TenantEventID) || ev.EventType == "" || ev.OccurredAt.IsZero() || ev.Channel == "" || ev.Subject.AccountRef == "" && ev.Subject.MSISDN == "" {
		write(w, 422, map[string]string{"error": "required_fields_missing_or_invalid"})
		return
	}
	if !oneOf(ev.EventType, "onboarding", "login", "device_change", "sim_change", "beneficiary_add", "funds_transfer", "loan_application", "limit_change", "agent_float_movement") || !oneOf(ev.Channel, "mobile_app", "ussd", "agent", "web", "loan_app", "api") {
		write(w, 422, map[string]string{"error": "unsupported_event_type_or_channel"})
		return
	}
	if ev.OccurredAt.After(time.Now().Add(5 * time.Minute)) {
		write(w, 422, map[string]string{"error": "occurred_at_in_future"})
		return
	}
	if ev.Subject.MSISDN != "" && !validMSISDN(ev.Subject.MSISDN) {
		write(w, 422, map[string]string{"error": "msisdn_must_be_e164"})
		return
	}
	if ev.Transaction.Amount < 0 {
		write(w, 422, map[string]string{"error": "amount_must_be_non_negative"})
		return
	}
	if ev.Transaction.Currency != "" && (len(ev.Transaction.Currency) != 3 || strings.ToUpper(ev.Transaction.Currency) != ev.Transaction.Currency || !asciiAlpha(ev.Transaction.Currency)) {
		write(w, 422, map[string]string{"error": "currency_must_be_iso_4217_alpha_code"})
		return
	}
	if ev.Signals.BalanceDrainRatio != nil && (*ev.Signals.BalanceDrainRatio < 0 || *ev.Signals.BalanceDrainRatio > 1) {
		write(w, 422, map[string]string{"error": "balance_drain_ratio_must_be_between_zero_and_one"})
		return
	}
	if ev.Signals.TransfersInWindow < 0 || ev.Signals.NewBeneficiariesInWindow < 0 || ev.Signals.WindowMinutes < 0 || ev.Signals.InboundFanIn < 0 || ev.Signals.AccountAgeDays != nil && *ev.Signals.AccountAgeDays < 0 {
		write(w, 422, map[string]string{"error": "signal_counts_must_be_non_negative"})
		return
	}
	if r.Header.Get("X-Kinga-Tenant") != "" && r.Header.Get("X-Kinga-Tenant") != cred.Tenant {
		write(w, 403, map[string]string{"error": "tenant_header_mismatch"})
		return
	}
	if !a.allow(cred.Tenant) {
		w.Header().Set("Retry-After", "60")
		write(w, 429, map[string]string{"error": "tenant_rate_limit_exceeded"})
		return
	}
	if r.Header.Get("Idempotency-Key") != ev.TenantEventID {
		write(w, 400, map[string]string{"error": "idempotency_key_must_match_tenant_event_id"})
		return
	}
	key := store.Key(cred.Tenant, ev.TenantEventID)
	unlock := a.lockIdempotencyKey(key)
	defer unlock()
	if prev, found, err := a.idem.Get(key, body); err != nil {
		write(w, 409, map[string]string{"error": "idempotency_conflict"})
		return
	} else if found {
		w.Header().Set("Idempotent-Replay", "true")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write(append(prev, '\n'))
		return
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 100*time.Millisecond)
	defer cancel()
	telcoSignal, telcoErr := a.telco.Lookup(ctx, ev.Subject.MSISDN)
	available := telcoErr == nil && telcoSignal.Available
	ev.Signals.TelcoAvailable = &available
	ev.Signals.TelcoSource = telcoSignal.Source
	ev.Signals.SIMSwapAgeMinutes = nil
	if available {
		ev.Signals.SIMSwapAgeMinutes = telcoSignal.SIMSwapAgeMinutes
	}
	a.features.Observe(cred.Tenant, &ev)
	result, err := a.engine.Assess(ctx, ev, cred.Tenant)
	if err != nil {
		write(w, 503, map[string]string{"error": "decision_engine_unavailable"})
		return
	}
	result.LatencyMS = time.Since(started).Milliseconds()
	a.recordDecision(result)
	featureSnapshot, _ := json.Marshal(ev.Signals)
	eid, err := a.evidence.Append(cred.Tenant, ev.TenantEventID, body, featureSnapshot, func(evidenceID string) ([]byte, error) { result.EvidenceID = evidenceID; return json.Marshal(result) })
	if err != nil {
		write(w, 503, map[string]string{"error": "evidence_unavailable"})
		return
	}
	if result.Decision != "ALLOW" {
		if _, err := a.cases.OpenCase(cred.Tenant, result); err != nil {
			write(w, http.StatusServiceUnavailable, map[string]string{"error": "case_store_unavailable"})
			return
		}
	}
	_ = eid
	response, _ := json.Marshal(result)
	a.idem.Put(key, body, response)
	w.Header().Set("X-Kinga-Tenant", cred.Tenant)
	write(w, 200, result)
}
func (a *API) recordDecision(r engine.Result) {
	a.metricMu.Lock()
	defer a.metricMu.Unlock()
	a.assessCount++
	a.decisionCounters[r.Decision]++
	a.latencyTotalMS += uint64(max(r.LatencyMS, 0))
	if r.Degraded {
		a.degradedCount++
	}
}
func (a *API) lockIdempotencyKey(key string) func() {
	a.keyMu.Lock()
	l := a.keyLocks[key]
	if l == nil {
		l = &keyLock{}
		a.keyLocks[key] = l
	}
	l.refs++
	a.keyMu.Unlock()
	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		a.keyMu.Lock()
		l.refs--
		if l.refs == 0 {
			delete(a.keyLocks, key)
		}
		a.keyMu.Unlock()
	}
}
func (a *API) allow(tenant string) bool {
	a.rateMu.Lock()
	defer a.rateMu.Unlock()
	now := time.Now()
	w := a.rates[tenant]
	if now.Sub(w.start) >= time.Minute {
		w = rateWindow{start: now}
	}
	if w.count >= 300 {
		a.rates[tenant] = w
		return false
	}
	w.count++
	a.rates[tenant] = w
	return true
}
func validMSISDN(s string) bool {
	if len(s) < 9 || len(s) > 16 || s[0] != '+' || s[1] == '0' {
		return false
	}
	for _, c := range s[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func oneOf(s string, allowed ...string) bool {
	for _, v := range allowed {
		if s == v {
			return true
		}
	}
	return false
}
func asciiAlpha(s string) bool {
	for _, c := range s {
		if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
			return false
		}
	}
	return true
}
func (a *API) verify(w http.ResponseWriter, _ *http.Request) {
	n, head, err := a.evidence.Verify()
	if err != nil {
		write(w, 500, map[string]any{"status": "failed", "records_checked": n, "error": err.Error()})
		return
	}
	write(w, 200, map[string]any{"status": "verified", "records_checked": n, "chain_head": head})
}

func (a *API) submitOutcome(w http.ResponseWriter, r *http.Request) {
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "application/json" {
		write(w, http.StatusUnsupportedMediaType, map[string]string{"error": "content_type_must_be_application_json"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 16*1024+1))
	if err != nil || len(body) > 16*1024 {
		write(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request_too_large"})
		return
	}
	credential, ok := auth.Verify(r, body, a.keys)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "invalid_api_credentials_or_signature"})
		return
	}
	timestamp, err := strconv.ParseInt(r.Header.Get("X-Kinga-Timestamp"), 10, 64)
	if err != nil || time.Since(time.Unix(timestamp, 0)) > 5*time.Minute || time.Until(time.Unix(timestamp, 0)) > 5*time.Minute {
		write(w, http.StatusUnauthorized, map[string]string{"error": "signature_timestamp_outside_window"})
		return
	}
	var submission outcomes.Submission
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&submission); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid_outcome", "detail": err.Error()})
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		write(w, http.StatusBadRequest, map[string]string{"error": "request_must_contain_one_json_object"})
		return
	}
	if submission.SubmissionID == "" || !eventID.MatchString(submission.SubmissionID) || submission.TenantEventID == "" || !eventID.MatchString(submission.TenantEventID) || submission.EvidenceID == "" || len(submission.EvidenceID) > 160 || submission.Reviewer == "" || len(submission.Reviewer) > 128 || len(submission.Notes) > 512 || submission.ObservedAt.IsZero() {
		write(w, http.StatusUnprocessableEntity, map[string]string{"error": "required_outcome_fields_missing_or_invalid"})
		return
	}
	if !oneOf(submission.Label, "confirmed_fraud", "false_positive", "legitimate", "unknown") {
		write(w, http.StatusUnprocessableEntity, map[string]string{"error": "unsupported_outcome_label"})
		return
	}
	if submission.LossKES != nil && (*submission.LossKES < 0 || math.IsNaN(*submission.LossKES) || math.IsInf(*submission.LossKES, 0)) {
		write(w, http.StatusUnprocessableEntity, map[string]string{"error": "loss_kes_must_be_finite_and_non_negative"})
		return
	}
	if submission.ObservedAt.After(time.Now().Add(5 * time.Minute)) {
		write(w, http.StatusUnprocessableEntity, map[string]string{"error": "observed_at_in_future"})
		return
	}
	if r.Header.Get("X-Kinga-Tenant") != "" && r.Header.Get("X-Kinga-Tenant") != credential.Tenant {
		write(w, http.StatusForbidden, map[string]string{"error": "tenant_header_mismatch"})
		return
	}
	if !a.allow(credential.Tenant) {
		w.Header().Set("Retry-After", "60")
		write(w, http.StatusTooManyRequests, map[string]string{"error": "tenant_rate_limit_exceeded"})
		return
	}
	if r.Header.Get("Idempotency-Key") != submission.SubmissionID {
		write(w, http.StatusBadRequest, map[string]string{"error": "idempotency_key_must_match_submission_id"})
		return
	}
	record, err := a.outcomes.Append(credential.Tenant, submission)
	if err != nil {
		if errors.Is(err, outcomes.ErrUnavailable) {
			write(w, http.StatusServiceUnavailable, map[string]string{"error": "outcome_store_unavailable"})
			return
		}
		write(w, http.StatusConflict, map[string]string{"error": "outcome_submission_conflict", "detail": err.Error()})
		return
	}
	w.Header().Set("X-Kinga-Tenant", credential.Tenant)
	write(w, http.StatusAccepted, record)
}

func (a *API) listCases(w http.ResponseWriter, r *http.Request) {
	credential, ok := signedCredential(r, nil, a.keys)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "invalid_api_credentials_or_signature"})
		return
	}
	if !a.allow(credential.Tenant) {
		w.Header().Set("Retry-After", "60")
		write(w, http.StatusTooManyRequests, map[string]string{"error": "tenant_rate_limit_exceeded"})
		return
	}
	status := r.URL.Query().Get("status")
	if status != "" && !oneOf(status, "open", "assigned", "released", "held") {
		write(w, http.StatusBadRequest, map[string]string{"error": "unsupported_case_status"})
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			write(w, http.StatusBadRequest, map[string]string{"error": "limit_must_be_between_1_and_100"})
			return
		}
		limit = parsed
	}
	w.Header().Set("X-Kinga-Tenant", credential.Tenant)
	write(w, http.StatusOK, map[string]any{"cases": a.cases.List(credential.Tenant, status, limit)})
}

func (a *API) applyCaseAction(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 8*1024+1))
	if err != nil || len(body) > 8*1024 {
		write(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request_too_large"})
		return
	}
	credential, ok := signedCredential(r, body, a.keys)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "invalid_api_credentials_or_signature"})
		return
	}
	if !a.allow(credential.Tenant) {
		w.Header().Set("Retry-After", "60")
		write(w, http.StatusTooManyRequests, map[string]string{"error": "tenant_rate_limit_exceeded"})
		return
	}
	var request struct {
		ActionID string `json:"action_id"`
		Type     string `json:"type"`
		Reviewer string `json:"reviewer"`
		Assignee string `json:"assignee,omitempty"`
		Note     string `json:"note,omitempty"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid_case_action", "detail": err.Error()})
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		write(w, http.StatusBadRequest, map[string]string{"error": "request_must_contain_one_json_object"})
		return
	}
	action := cases.Action{ActionID: request.ActionID, Type: request.Type, Reviewer: request.Reviewer, Assignee: request.Assignee, Note: request.Note}
	if err := cases.ValidateAction(action); err != nil {
		write(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	if r.Header.Get("Idempotency-Key") != action.ActionID {
		write(w, http.StatusBadRequest, map[string]string{"error": "idempotency_key_must_match_action_id"})
		return
	}
	updated, err := a.cases.Apply(credential.Tenant, r.PathValue("caseID"), action)
	if err != nil {
		status := http.StatusConflict
		if errors.Is(err, os.ErrNotExist) {
			status = http.StatusNotFound
		} else if errors.Is(err, cases.ErrUnavailable) {
			status = http.StatusServiceUnavailable
		}
		write(w, status, map[string]string{"error": "case_action_rejected", "detail": err.Error()})
		return
	}
	w.Header().Set("X-Kinga-Tenant", credential.Tenant)
	write(w, http.StatusOK, updated)
}

func (a *API) exportCaseEvidence(w http.ResponseWriter, r *http.Request) {
	credential, ok := signedCredential(r, nil, a.keys)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "invalid_api_credentials_or_signature"})
		return
	}
	if !a.allow(credential.Tenant) {
		w.Header().Set("Retry-After", "60")
		write(w, http.StatusTooManyRequests, map[string]string{"error": "tenant_rate_limit_exceeded"})
		return
	}
	caseRecord, err := a.cases.Get(credential.Tenant, r.PathValue("caseID"))
	if err != nil {
		write(w, http.StatusNotFound, map[string]string{"error": "case_not_found"})
		return
	}
	evidenceRecord, err := a.evidence.Find(credential.Tenant, caseRecord.EvidenceID)
	if err != nil {
		write(w, http.StatusServiceUnavailable, map[string]string{"error": "evidence_unavailable_or_invalid"})
		return
	}
	w.Header().Set("X-Kinga-Tenant", credential.Tenant)
	w.Header().Set("Content-Disposition", "attachment; filename=case-evidence.json")
	write(w, http.StatusOK, map[string]any{"export_version": "1", "generated_at": time.Now().UTC(), "case": caseRecord, "evidence": evidenceRecord})
}
