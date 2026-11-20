package ingest

import (
	"bytes"
	"strings"
	"testing"
)

func TestConvertRowAndProcessCSV(t *testing.T) {
	csvData := `tenant_event_id,event_type,occurred_at,channel,msisdn,account_ref,amount,currency,rail,beneficiary_type,beneficiary_ref,sim_swap_age_minutes,device_first_seen,confirmed_fraud
evt_01,funds_transfer,2026-11-20T10:00:00Z,mobile_app,0712345678,acc_99,50000,KES,pesalink,bank_account,ben_10,30,true,true
evt_02,funds_transfer,2026-11-20T10:05:00Z,ussd,+254799999999,acc_100,1000,KES,mpesa,wallet,ben_20,,,false
`

	var out bytes.Buffer
	count, err := ProcessCSV(strings.NewReader(csvData), &out)
	if err != nil {
		t.Fatalf("ProcessCSV failed: %v", err)
	}
	if count != 2 {
		t.Fatalf("Expected 2 records, got %d", count)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("Expected 2 lines, got %d", len(lines))
	}

	// Verify row 1 normalized msisdn: 0712345678 -> +254712345678
	if !strings.Contains(lines[0], `"+254712345678"`) {
		t.Errorf("Expected normalized MSISDN +254712345678 in row 1, got %s", lines[0])
	}
	// Verify row 1 confirmed fraud is true
	if !strings.Contains(lines[0], `"confirmed_fraud":true`) {
		t.Errorf("Expected confirmed_fraud:true in row 1, got %s", lines[0])
	}

	// Verify row 2 confirmed fraud is false
	if !strings.Contains(lines[1], `"confirmed_fraud":false`) {
		t.Errorf("Expected confirmed_fraud:false in row 2, got %s", lines[1])
	}
}
