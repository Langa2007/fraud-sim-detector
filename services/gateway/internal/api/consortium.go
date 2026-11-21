package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (a *API) tokenizeIdentifier(w http.ResponseWriter, r *http.Request) {
	body, ok := readBounded(w, r, 8*1024)
	if !ok {
		return
	}
	cred, ok := signedCredential(r, body, a.keys)
	if !ok {
		write(w, 401, map[string]string{"error": "invalid_api_credentials_or_signature"})
		return
	}
	var req struct {
		Namespace  string `json:"namespace"`
		Value      string `json:"value"`
		Scope      string `json:"scope"`
		KeyVersion string `json:"key_version,omitempty"`
	}
	d := json.NewDecoder(strings.NewReader(string(body)))
	d.DisallowUnknownFields()
	if err := d.Decode(&req); err != nil {
		write(w, 400, map[string]string{"error": "invalid_request"})
		return
	}
	if req.Scope == "" {
		req.Scope = "network"
	}
	payload := map[string]any{"namespace": req.Namespace, "value": req.Value, "scope": req.Scope}
	if req.KeyVersion != "" {
		payload["key_version"] = req.KeyVersion
	}
	if req.Scope == "tenant" {
		payload["tenant"] = cred.Tenant
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		write(w, 500, map[string]string{"error": "tokenization_unavailable"})
		return
	}
	endpoint, err := url.JoinPath(a.cfg.TokenizerURL, "v1/tokenize")
	if err != nil {
		write(w, 503, map[string]string{"error": "tokenization_unavailable"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 750*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		write(w, 503, map[string]string{"error": "tokenization_unavailable"})
		return
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Tokenizer-Key", a.cfg.TokenizerCallerKey)
	response, err := (&http.Client{Timeout: 750 * time.Millisecond}).Do(request)
	if err != nil {
		write(w, 503, map[string]string{"error": "tokenization_unavailable"})
		return
	}
	defer response.Body.Close()
	result, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(result) > 4096 {
		write(w, 503, map[string]string{"error": "tokenization_unavailable"})
		return
	}
	if response.StatusCode != http.StatusOK {
		write(w, 422, map[string]string{"error": "tokenization_rejected"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(result)
}

func (a *API) contributeIndicator(w http.ResponseWriter, r *http.Request) {
	body, ok := readBounded(w, r, 16*1024)
	if !ok {
		return
	}
	cred, ok := signedCredential(r, body, a.keys)
	if !ok {
		write(w, 401, map[string]string{"error": "invalid_api_credentials_or_signature"})
		return
	}
	var req struct {
		Namespace  string  `json:"namespace"`
		Token      string  `json:"token"`
		Confidence float64 `json:"confidence"`
		TTLHours   int     `json:"ttl_hours"`
	}
	d := json.NewDecoder(strings.NewReader(string(body)))
	d.DisallowUnknownFields()
	if err := d.Decode(&req); err != nil {
		write(w, 400, map[string]string{"error": "invalid_request"})
		return
	}
	if req.TTLHours == 0 {
		req.TTLHours = 24 * 30
	}
	record, err := a.consortium.Contribute(cred.Tenant, req.Namespace, req.Token, req.Confidence, time.Duration(req.TTLHours)*time.Hour)
	if err != nil {
		write(w, 422, map[string]string{"error": err.Error()})
		return
	}
	write(w, 201, map[string]any{"indicator_id": record.Indicator.ID, "status": record.Indicator.Status, "expires_at": record.Indicator.ExpiresAt})
}

func (a *API) queryIndicator(w http.ResponseWriter, r *http.Request) {
	cred, ok := signedCredential(r, nil, a.keys)
	if !ok {
		write(w, 401, map[string]string{"error": "invalid_api_credentials_or_signature"})
		return
	}
	result, found := a.consortium.Query(r.PathValue("namespace"), r.PathValue("token"), time.Now().UTC())
	if !found {
		write(w, 404, map[string]string{"error": "indicator_not_found_or_below_privacy_threshold"})
		return
	}
	_ = cred
	write(w, 200, result)
}

func (a *API) disputeIndicator(w http.ResponseWriter, r *http.Request) {
	body, ok := readBounded(w, r, 8*1024)
	if !ok {
		return
	}
	cred, ok := signedCredential(r, body, a.keys)
	if !ok {
		write(w, 401, map[string]string{"error": "invalid_api_credentials_or_signature"})
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		write(w, 400, map[string]string{"error": "invalid_request"})
		return
	}
	record, err := a.consortium.Dispute(cred.Tenant, r.PathValue("namespace"), r.PathValue("token"), req.Reason)
	if err != nil {
		write(w, 422, map[string]string{"error": err.Error()})
		return
	}
	write(w, 202, map[string]string{"status": record.Indicator.Status})
}

func (a *API) resolveIndicator(w http.ResponseWriter, r *http.Request) {
	if a.cfg.ConsortiumAdminToken == "" || r.Header.Get("Authorization") != "Bearer "+a.cfg.ConsortiumAdminToken {
		write(w, 401, map[string]string{"error": "governance_authorization_required"})
		return
	}
	body, ok := readBounded(w, r, 8*1024)
	if !ok {
		return
	}
	var req struct {
		Action string `json:"action"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		write(w, 400, map[string]string{"error": "invalid_request"})
		return
	}
	record, err := a.consortium.Resolve(r.PathValue("namespace"), r.PathValue("token"), req.Action, req.Reason)
	if err != nil {
		write(w, 422, map[string]string{"error": err.Error()})
		return
	}
	write(w, 200, map[string]string{"status": record.Indicator.Status})
}

func (a *API) consortiumMember(w http.ResponseWriter, r *http.Request) {
	cred, ok := signedCredential(r, nil, a.keys)
	if !ok {
		write(w, 401, map[string]string{"error": "invalid_api_credentials_or_signature"})
		return
	}
	write(w, 200, map[string]any{"tenant": cred.Tenant, "contributions": a.consortium.MemberContributionCount(cred.Tenant)})
}

func (a *API) lookupGraph(w http.ResponseWriter, r *http.Request) {
	if _, ok := signedCredential(r, nil, a.keys); !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "invalid_api_credentials_or_signature"})
		return
	}
	if a.graph == nil {
		write(w, http.StatusServiceUnavailable, map[string]string{"error": "graph_snapshot_unavailable"})
		return
	}
	feature, ok := a.graph.Lookup(r.PathValue("namespace"), r.PathValue("token"), a.cfg.ConsortiumMinConfirmations)
	if !ok {
		write(w, http.StatusNotFound, map[string]string{"error": "graph_feature_not_found_or_below_privacy_threshold"})
		return
	}
	write(w, http.StatusOK, feature)
}
