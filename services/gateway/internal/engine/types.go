package engine

import "time"

type Event struct {
	TenantEventID string    `json:"tenant_event_id"`
	EventType     string    `json:"event_type"`
	OccurredAt    time.Time `json:"occurred_at"`
	Channel       string    `json:"channel"`
	Subject       struct {
		MSISDN      string `json:"msisdn,omitempty"`
		AccountRef  string `json:"account_ref,omitempty"`
		CustomerRef string `json:"customer_ref,omitempty"`
	} `json:"subject"`
	Device struct {
		DeviceID   string `json:"device_id,omitempty"`
		IP         string `json:"ip,omitempty"`
		AppVersion string `json:"app_version,omitempty"`
	} `json:"device,omitempty"`
	Transaction struct {
		Amount      float64 `json:"amount,omitempty"`
		Currency    string  `json:"currency,omitempty"`
		Rail        string  `json:"rail,omitempty"`
		Beneficiary struct {
			Type string `json:"type,omitempty"`
			Ref  string `json:"ref,omitempty"`
		} `json:"beneficiary,omitempty"`
	} `json:"transaction,omitempty"`
	Signals struct {
		SIMSwapAgeMinutes        *int     `json:"sim_swap_age_minutes,omitempty"`
		TelcoAvailable           *bool    `json:"telco_available,omitempty"`
		TelcoSource              string   `json:"telco_source,omitempty"`
		DeviceFirstSeen          bool     `json:"device_first_seen,omitempty"`
		BeneficiaryFirstSeen     bool     `json:"beneficiary_first_seen,omitempty"`
		BalanceDrainRatio        *float64 `json:"balance_drain_ratio,omitempty"`
		TransfersInWindow        int      `json:"transfers_in_window,omitempty"`
		NewBeneficiariesInWindow int      `json:"new_beneficiaries_in_window,omitempty"`
		WindowMinutes            int      `json:"window_minutes,omitempty"`
		AccountAgeDays           *int     `json:"account_age_days,omitempty"`
		InboundFanIn             int      `json:"inbound_fan_in,omitempty"`
		RapidOutbound            bool     `json:"rapid_outbound,omitempty"`
	} `json:"signals,omitempty"`
}

type Reason struct {
	Code   string  `json:"code"`
	Detail string  `json:"detail"`
	Weight float64 `json:"weight"`
}
type Result struct {
	Decision           string   `json:"decision"`
	Mode               string   `json:"mode"`
	RiskScore          float64  `json:"risk_score"`
	RiskBand           string   `json:"risk_band"`
	Reasons            []Reason `json:"reasons"`
	RecommendedActions []string `json:"recommended_actions"`
	Degraded           bool     `json:"degraded"`
	TenantEventID      string   `json:"tenant_event_id"`
	PolicyVersion      string   `json:"policy_version"`
	ModelVersion       *string  `json:"model_version"`
	LatencyMS          int64    `json:"latency_ms"`
	EvidenceID         string   `json:"evidence_id,omitempty"`
}
