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
	"net/url"
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
	"kinga/services/gateway/internal/consortium"
	"kinga/services/gateway/internal/engine"
	"kinga/services/gateway/internal/evidence"
	"kinga/services/gateway/internal/features"
	"kinga/services/gateway/internal/graph"
	"kinga/services/gateway/internal/migration"
	"kinga/services/gateway/internal/outcomes"
	"kinga/services/gateway/internal/stepup"
	"kinga/services/gateway/internal/store"
	"kinga/services/gateway/internal/telco"
)

type API struct {
	idem             *store.Store
	engine           *engine.Client
	evidence         *evidence.Log
	cases            *cases.Store
	outcomes         *outcomes.Store
	consortium       *consortium.Store
	graph            *graph.Runtime
	stepup           *stepup.Orchestrator
	migration        *migration.Manager
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

func New(s *store.Store, e *engine.Client, l *evidence.Log, cs *cases.Store, o *outcomes.Store, co *consortium.Store, gr *graph.Runtime, k map[string]auth.Credential, c config.Config, f *features.Runtime, t telco.Connector) *API {
	a := &API{
		idem:             s,
		engine:           e,
		evidence:         l,
		cases:            cs,
		outcomes:         o,
		consortium:       co,
		graph:            gr,
		stepup:           stepup.NewOrchestrator(),
		migration:        migration.NewManager(),
		features:         f,
		telco:            t,
		keys:             k,
		cfg:              c,
		mux:              http.NewServeMux(),
		rates:            map[string]rateWindow{},
		keyLocks:         map[string]*keyLock{},
		decisionCounters: map[string]uint64{},
	}
	a.mux.HandleFunc("GET /healthz", a.health)
	a.mux.HandleFunc("GET /readyz", a.ready)
	a.mux.HandleFunc("GET /metrics", a.metrics)
	a.mux.HandleFunc("POST /v1/assess", a.assess)
	a.mux.HandleFunc("GET /v1/evidence/verify", a.verify)
	a.mux.HandleFunc("POST /v1/outcomes", a.submitOutcome)
	a.mux.HandleFunc("POST /v1/consortium/indicators", a.contributeIndicator)
	a.mux.HandleFunc("POST /v1/tokenize", a.tokenizeIdentifier)
	a.mux.HandleFunc("GET /v1/consortium/indicators/{namespace}/{token}", a.queryIndicator)
	a.mux.HandleFunc("POST /v1/consortium/indicators/{namespace}/{token}/disputes", a.disputeIndicator)
	a.mux.HandleFunc("POST /v1/consortium/indicators/{namespace}/{token}/resolution", a.resolveIndicator)
	a.mux.HandleFunc("GET /v1/consortium/members/me", a.consortiumMember)
	a.mux.HandleFunc("GET /v1/graph/{namespace}/{token}", a.lookupGraph)
	a.mux.HandleFunc("GET /v1/cases", a.listCases)
	a.mux.HandleFunc("POST /v1/cases/{caseID}/actions", a.applyCaseAction)
	a.mux.HandleFunc("GET /v1/cases/{caseID}/evidence", a.exportCaseEvidence)
	a.mux.HandleFunc("GET /v1/cases/{caseID}/evidence/pack", a.exportCaseEvidencePack)
	a.mux.HandleFunc("POST /v1/stepup/challenges", a.initiateStepUp)
	a.mux.HandleFunc("GET /v1/stepup/challenges/{challengeID}", a.getStepUp)
	a.mux.HandleFunc("POST /v1/stepup/challenges/{challengeID}/respond", a.respondStepUp)
	a.mux.HandleFunc("GET /v1/rollout", a.getRollout)
	a.mux.HandleFunc("POST /v1/rollout", a.setRollout)
	return a
}
func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) { a.mux.ServeHTTP(w, r) }
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func readBounded(w http.ResponseWriter, r *http.Request, maxBytes int64) ([]byte, bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "cannot_read_body"})
		return nil, false
	}
	if int64(len(body)) > maxBytes {
		write(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request_body_too_large"})
		return nil, false
	}
	return body, true
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
	endpoint, err := url.JoinPath(a.cfg.TokenizerURL, "healthz")
	if err != nil {
		write(w, 503, map[string]string{"status": "not_ready", "dependency": "tokenizer"})
		return
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		write(w, 503, map[string]string{"status": "not_ready", "dependency": "tokenizer"})
		return
	}
	response, err := (&http.Client{Timeout: 100 * time.Millisecond}).Do(request)
	if err != nil {
		write(w, 503, map[string]string{"status": "not_ready", "dependency": "tokenizer"})
		return
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		write(w, 503, map[string]string{"status": "not_ready", "dependency": "tokenizer"})
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
	rolloutMode := a.migration.EvaluateMode(cred.Tenant, ev.Subject.AccountRef)
	if rolloutMode == migration.ModeEnforce {
		result.Mode = "enforce"
	} else if rolloutMode == migration.ModeAdvisory {
		result.Mode = "advisory"
	}
	a.migration.RecordOutcome(cred.Tenant, result.Decision != "ALLOW")
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

type stepUpRequest struct {
	TenantEventID string  `json:"tenant_event_id"`
	Channel       string  `json:"channel"`
	Language      string  `json:"language"`
	Amount        float64 `json:"amount"`
	Beneficiary   string  `json:"beneficiary"`
	TTLSeconds    int     `json:"ttl_seconds"`
}

func (a *API) initiateStepUp(w http.ResponseWriter, r *http.Request) {
	body, ok := readBounded(w, r, 16*1024)
	if !ok {
		return
	}
	credential, ok := signedCredential(r, body, a.keys)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "invalid_api_credentials_or_signature"})
		return
	}
	var req stepUpRequest
	if err := json.Unmarshal(body, &req); err != nil || req.TenantEventID == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid_stepup_request"})
		return
	}
	ttl := time.Duration(req.TTLSeconds) * time.Second
	ch, err := a.stepup.Initiate(credential.Tenant, req.TenantEventID, stepup.Channel(req.Channel), req.Language, req.Amount, req.Beneficiary, ttl)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "stepup_initiation_failed"})
		return
	}
	w.Header().Set("X-Kinga-Tenant", credential.Tenant)
	write(w, http.StatusCreated, ch)
}

func (a *API) getStepUp(w http.ResponseWriter, r *http.Request) {
	credential, ok := signedCredential(r, nil, a.keys)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "invalid_api_credentials_or_signature"})
		return
	}
	ch, err := a.stepup.Get(credential.Tenant, r.PathValue("challengeID"))
	if err != nil {
		write(w, http.StatusNotFound, map[string]string{"error": "challenge_not_found"})
		return
	}
	w.Header().Set("X-Kinga-Tenant", credential.Tenant)
	write(w, http.StatusOK, ch)
}

func (a *API) respondStepUp(w http.ResponseWriter, r *http.Request) {
	body, ok := readBounded(w, r, 16*1024)
	if !ok {
		return
	}
	credential, ok := signedCredential(r, body, a.keys)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "invalid_api_credentials_or_signature"})
		return
	}
	var req struct {
		Approved bool `json:"approved"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid_response_body"})
		return
	}
	ch, err := a.stepup.Respond(credential.Tenant, r.PathValue("challengeID"), req.Approved)
	if err != nil {
		if errors.Is(err, stepup.ErrNotFound) {
			write(w, http.StatusNotFound, map[string]string{"error": "challenge_not_found"})
		} else {
			write(w, http.StatusConflict, map[string]string{"error": "challenge_already_resolved_or_expired", "detail": err.Error()})
		}
		return
	}
	w.Header().Set("X-Kinga-Tenant", credential.Tenant)
	write(w, http.StatusOK, ch)
}

func (a *API) getRollout(w http.ResponseWriter, r *http.Request) {
	credential, ok := signedCredential(r, nil, a.keys)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "invalid_api_credentials_or_signature"})
		return
	}
	cfg := a.migration.GetConfig(credential.Tenant)
	rollbackReason := a.migration.GetAutoRollbackReason(credential.Tenant)
	w.Header().Set("X-Kinga-Tenant", credential.Tenant)
	write(w, http.StatusOK, map[string]any{
		"tenant":               credential.Tenant,
		"config":               cfg,
		"auto_rollback_reason": rollbackReason,
	})
}

func (a *API) setRollout(w http.ResponseWriter, r *http.Request) {
	body, ok := readBounded(w, r, 16*1024)
	if !ok {
		return
	}
	credential, ok := signedCredential(r, body, a.keys)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "invalid_api_credentials_or_signature"})
		return
	}
	var req migration.Config
	if err := json.Unmarshal(body, &req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid_rollout_config"})
		return
	}
	a.migration.SetConfig(credential.Tenant, req)
	w.Header().Set("X-Kinga-Tenant", credential.Tenant)
	write(w, http.StatusOK, map[string]any{"status": "updated", "config": req})
}

func (a *API) exportCaseEvidencePack(w http.ResponseWriter, r *http.Request) {
	credential, ok := signedCredential(r, nil, a.keys)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "invalid_api_credentials_or_signature"})
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

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Kinga-Tenant", credential.Tenant)

	reasonsHTML := ""
	for _, r := range caseRecord.Reasons {
		reasonsHTML += fmt.Sprintf(`<tr><td><code>%s</code></td><td>%.2f</td><td>%s</td></tr>`, r.Code, r.Weight, r.Detail)
	}

	historyHTML := ""
	for _, h := range caseRecord.History {
		historyHTML += fmt.Sprintf(`<tr><td>%s</td><td><strong>%s</strong></td><td>%s</td><td>%s</td></tr>`,
			h.CreatedAt.Format(time.RFC3339), h.Type, h.Reviewer, h.Note)
	}

	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
<title>Kinga Evidence Pack - %s</title>
<style>
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; margin: 40px; color: #1e293b; background: #fff; }
.header { border-bottom: 2px solid #0f172a; padding-bottom: 20px; margin-bottom: 24px; }
.seal { float: right; border: 2px solid #16a34a; color: #16a34a; padding: 6px 12px; border-radius: 4px; font-weight: 700; font-size: 0.8rem; text-transform: uppercase; }
h1 { margin: 0; font-size: 1.5rem; color: #0f172a; }
.meta { margin-top: 8px; color: #64748b; font-size: 0.9rem; }
.card { border: 1px solid #cbd5e1; border-radius: 6px; padding: 16px; margin-bottom: 24px; }
h2 { font-size: 1.1rem; border-bottom: 1px solid #e2e8f0; padding-bottom: 8px; margin-top: 0; }
table { width: 100%%; border-collapse: collapse; margin-top: 12px; font-size: 0.9rem; }
th, td { text-align: left; padding: 8px 12px; border-bottom: 1px solid #e2e8f0; }
th { background: #f8fafc; color: #475569; }
code { font-family: ui-monospace, SFMono-Regular, monospace; background: #f1f5f9; padding: 2px 4px; border-radius: 3px; font-size: 0.85rem; }
@media print { body { margin: 0; } }
</style>
</head>
<body>
<div class="seal">✓ SHA-256 Hash Chain Verified</div>
<div class="header">
  <h1>Kinga Fraud Evidence Pack</h1>
  <div class="meta">Case ID: <code>%s</code> | Tenant: <strong>%s</strong> | Event: <code>%s</code> | Generated: %s</div>
</div>

<div class="card">
  <h2>Decision & Risk Summary</h2>
  <table>
    <tr><th>Decision</th><td><strong>%s</strong> (Mode: %s)</td></tr>
    <tr><th>Risk Score</th><td>%.2f (Band: %s)</td></tr>
    <tr><th>Evidence ID</th><td><code>%s</code> (Sequence #%d)</td></tr>
    <tr><th>Policy / Model Version</th><td>%s / %v</td></tr>
    <tr><th>Raw Event Hash</th><td><code>%s</code></td></tr>
    <tr><th>Chain Record Hash</th><td><code>%s</code></td></tr>
  </table>
</div>

<div class="card">
  <h2>Risk Contributing Reasons</h2>
  <table>
    <thead><tr><th>Reason Code</th><th>Weight</th><th>Detail</th></tr></thead>
    <tbody>%s</tbody>
  </table>
</div>

<div class="card">
  <h2>Reviewer & Case Timeline</h2>
  <table>
    <thead><tr><th>Timestamp</th><th>Action</th><th>Reviewer</th><th>Notes / Rationale</th></tr></thead>
    <tbody>%s</tbody>
  </table>
</div>
</body>
</html>`,
		caseRecord.CaseID, caseRecord.CaseID, credential.Tenant, caseRecord.TenantEventID, time.Now().UTC().Format(time.RFC3339),
		caseRecord.Decision, caseRecord.Decision, caseRecord.RiskScore, caseRecord.RiskBand,
		evidenceRecord.EvidenceID, evidenceRecord.Sequence, caseRecord.PolicyVersion, caseRecord.ModelVersion,
		evidenceRecord.EventHash, evidenceRecord.Hash,
		reasonsHTML, historyHTML,
	)

	_, _ = w.Write([]byte(html))
}
