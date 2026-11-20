package store

import (
	"testing"
	"time"
)

func TestStoreIdempotency(t *testing.T) {
	s := New("100ms")
	k := Key("tenantA", "evt_001")
	body := []byte(`{"event":"test"}`)
	resp := []byte(`{"decision":"ALLOW"}`)

	// Not found initially
	val, found, err := s.Get(k, body)
	if err != nil || found || val != nil {
		t.Fatalf("expected not found, got found=%v err=%v", found, err)
	}

	// Put
	s.Put(k, body, resp)

	// Hit
	val, found, err = s.Get(k, body)
	if err != nil || !found || string(val) != string(resp) {
		t.Fatalf("expected hit, got %s, found=%v, err=%v", string(val), found, err)
	}

	// Conflict with different body
	_, _, err = s.Get(k, []byte(`{"event":"different"}`))
	if err == nil {
		t.Fatalf("expected conflict error on mismatched payload, got nil")
	}

	// Expire TTL
	time.Sleep(120 * time.Millisecond)
	val, found, err = s.Get(k, body)
	if err != nil || found || val != nil {
		t.Fatalf("expected expired entry to not be found")
	}
}
