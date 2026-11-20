package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"kinga/services/gateway/internal/auth"
	"kinga/services/gateway/internal/config"
	"kinga/services/gateway/internal/engine"
	"kinga/services/gateway/internal/evidence"
	"kinga/services/gateway/internal/features"
	"kinga/services/gateway/internal/store"
	"kinga/services/gateway/internal/telco"
)

type API struct {
	idem             *store.Store
	engine           *engine.Client
	evidence         *evidence.Log
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

func New(s *store.Store, e *engine.Client, l *evidence.Log, k map[string]auth.Credential, c config.Config, f *features.Runtime, t telco.Connector) *API {
	a := &API{idem: s, engine: e, evidence: l, features: f, telco: t, keys: k, cfg: c, mux: http.NewServeMux(), rates: map[string]rateWindow{}, keyLocks: map[string]*keyLock{}, decisionCounters: map[string]uint64{}}
	a.mux.HandleFunc("GET /healthz", a.health)
	a.mux.HandleFunc("GET /readyz", a.ready)
	a.mux.HandleFunc("GET /metrics", a.metrics)
	a.mux.HandleFunc("POST /v1/assess", a.assess)
	a.mux.HandleFunc("GET /v1/evidence/verify", a.verify)
	return a
}
func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) { a.mux.ServeHTTP(w, r) }
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
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
