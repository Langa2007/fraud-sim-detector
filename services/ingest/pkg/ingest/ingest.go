package ingest

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"kinga/services/internal/norm"
)

type Event struct {
	TenantEventID  string       `json:"tenant_event_id"`
	EventType      string       `json:"event_type"`
	OccurredAt     string       `json:"occurred_at"`
	Channel        string       `json:"channel"`
	Subject        Subject      `json:"subject"`
	Device         *Device      `json:"device,omitempty"`
	Transaction    *Transaction `json:"transaction,omitempty"`
	Signals        *Signals     `json:"signals,omitempty"`
	ConfirmedFraud *bool        `json:"confirmed_fraud,omitempty"`
}

type Subject struct {
	MSISDN      string `json:"msisdn,omitempty"`
	AccountRef  string `json:"account_ref,omitempty"`
	CustomerRef string `json:"customer_ref,omitempty"`
}

type Device struct {
	DeviceID   string `json:"device_id,omitempty"`
	IP         string `json:"ip,omitempty"`
	AppVersion string `json:"app_version,omitempty"`
}

type Transaction struct {
	Amount      float64      `json:"amount,omitempty"`
	Currency    string       `json:"currency,omitempty"`
	Rail        string       `json:"rail,omitempty"`
	Beneficiary *Beneficiary `json:"beneficiary,omitempty"`
}

type Beneficiary struct {
	Type string `json:"type,omitempty"`
	Ref  string `json:"ref,omitempty"`
}

type Signals struct {
	SIMSwapAgeMinutes        *int     `json:"sim_swap_age_minutes,omitempty"`
	DeviceFirstSeen          *bool    `json:"device_first_seen,omitempty"`
	BeneficiaryFirstSeen     *bool    `json:"beneficiary_first_seen,omitempty"`
	BalanceDrainRatio        *float64 `json:"balance_drain_ratio,omitempty"`
	TransfersInWindow        *int     `json:"transfers_in_window,omitempty"`
	NewBeneficiariesInWindow *int     `json:"new_beneficiaries_in_window,omitempty"`
	WindowMinutes            *int     `json:"window_minutes,omitempty"`
	AccountAgeDays           *int     `json:"account_age_days,omitempty"`
	InboundFanIn             *int     `json:"inbound_fan_in,omitempty"`
	RapidOutbound            *bool    `json:"rapid_outbound,omitempty"`
}

func parseBool(v string) (*bool, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return nil, nil
	}
	switch v {
	case "true", "1", "yes", "t":
		b := true
		return &b, nil
	case "false", "0", "no", "f":
		b := false
		return &b, nil
	default:
		return nil, fmt.Errorf("invalid boolean %q", v)
	}
}

// ConvertRow maps a CSV header-keyed row into a canonical Assessment Event,
// normalizing the MSISDN to E.164.
func ConvertRow(row map[string]string) (*Event, error) {
	val := func(k string) string { return strings.TrimSpace(row[k]) }

	eventID := val("tenant_event_id")
	if eventID == "" {
		return nil, fmt.Errorf("tenant_event_id is required")
	}

	rawMSISDN := val("msisdn")
	normalizedMSISDN := ""
	if rawMSISDN != "" {
		normed, err := norm.NormalizeE164(rawMSISDN)
		if err != nil {
			return nil, fmt.Errorf("msisdn normalization error for %q: %w", rawMSISDN, err)
		}
		normalizedMSISDN = normed
	}

	ev := &Event{
		TenantEventID: eventID,
		EventType:     val("event_type"),
		OccurredAt:    val("occurred_at"),
		Channel:       val("channel"),
		Subject: Subject{
			MSISDN:      normalizedMSISDN,
			AccountRef:  val("account_ref"),
			CustomerRef: val("customer_ref"),
		},
	}

	if dID, ip, ver := val("device_id"), val("ip"), val("app_version"); dID != "" || ip != "" || ver != "" {
		ev.Device = &Device{DeviceID: dID, IP: ip, AppVersion: ver}
	}

	amtStr := val("amount")
	if amtStr != "" || val("currency") != "" || val("rail") != "" || val("beneficiary_ref") != "" {
		tx := &Transaction{
			Currency: val("currency"),
			Rail:     val("rail"),
		}
		if amtStr != "" {
			amt, err := strconv.ParseFloat(amtStr, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid amount %q: %w", amtStr, err)
			}
			tx.Amount = amt
		}
		if bRef := val("beneficiary_ref"); bRef != "" {
			tx.Beneficiary = &Beneficiary{
				Type: val("beneficiary_type"),
				Ref:  bRef,
			}
		}
		ev.Transaction = tx
	}

	sig := &Signals{}
	hasSig := false
	if v := val("sim_swap_age_minutes"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid sim_swap_age_minutes %q", v)
		}
		sig.SIMSwapAgeMinutes = &n
		hasSig = true
	}
	if v := val("balance_drain_ratio"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid balance_drain_ratio %q", v)
		}
		sig.BalanceDrainRatio = &f
		hasSig = true
	}
	if b, err := parseBool(val("device_first_seen")); err != nil {
		return nil, err
	} else if b != nil {
		sig.DeviceFirstSeen = b
		hasSig = true
	}
	if b, err := parseBool(val("beneficiary_first_seen")); err != nil {
		return nil, err
	} else if b != nil {
		sig.BeneficiaryFirstSeen = b
		hasSig = true
	}
	if b, err := parseBool(val("rapid_outbound")); err != nil {
		return nil, err
	} else if b != nil {
		sig.RapidOutbound = b
		hasSig = true
	}
	if hasSig {
		ev.Signals = sig
	}

	if b, err := parseBool(val("confirmed_fraud")); err != nil {
		return nil, err
	} else if b != nil {
		ev.ConfirmedFraud = b
	}

	return ev, nil
}

// ProcessCSV reads CSV rows from in and writes JSON Lines canonical events to out.
func ProcessCSV(in io.Reader, out io.Writer) (int, error) {
	reader := csv.NewReader(in)
	headers, err := reader.Read()
	if err != nil {
		return 0, err
	}

	count := 0
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return count, err
		}

		row := make(map[string]string, len(headers))
		for i, h := range headers {
			if i < len(record) {
				row[strings.TrimSpace(h)] = record[i]
			}
		}

		ev, err := ConvertRow(row)
		if err != nil {
			return count, fmt.Errorf("row %d: %w", count+2, err)
		}

		line, err := json.Marshal(ev)
		if err != nil {
			return count, err
		}
		if _, err := out.Write(append(line, '\n')); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// ConvertCSVFile converts an on-disk CSV file to a JSONL file.
func ConvertCSVFile(srcPath, dstPath string) (int, error) {
	src, err := os.Open(srcPath)
	if err != nil {
		return 0, err
	}
	defer src.Close()

	dst, err := os.Create(dstPath)
	if err != nil {
		return 0, err
	}
	defer dst.Close()

	return ProcessCSV(src, dst)
}
