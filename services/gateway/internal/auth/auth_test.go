package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseKeys(t *testing.T) {
	raw := "demo:demo-key:demo-secret,tenantB:key-b:secret-b"
	keys := ParseKeys(raw)
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
	c, ok := keys["demo-key"]
	if !ok || c.Tenant != "demo" || string(c.Secret) != "demo-secret" {
		t.Errorf("unexpected cred: %+v", c)
	}
}

func TestSignAndVerify(t *testing.T) {
	keys := map[string]Credential{
		"my-key": {Tenant: "tenantA", Secret: []byte("my-super-secret")},
	}

	timestamp := "1732095000"
	body := `{"tenant_event_id":"evt_123"}`
	sig := Sign(keys["my-key"].Secret, timestamp, body)

	req := httptest.NewRequest(http.MethodPost, "/v1/assess", nil)
	req.Header.Set("X-Kinga-Key", "my-key")
	req.Header.Set("X-Kinga-Timestamp", timestamp)
	req.Header.Set("X-Kinga-Signature", sig)

	cred, ok := Verify(req, []byte(body), keys)
	if !ok {
		t.Fatalf("expected signature verification to pass")
	}
	if cred.Tenant != "tenantA" {
		t.Errorf("expected tenantA, got %s", cred.Tenant)
	}

	// Tampered body
	_, ok = Verify(req, []byte(`{"tenant_event_id":"tampered"}`), keys)
	if ok {
		t.Errorf("expected verification to fail for tampered body")
	}

	// Invalid signature
	req.Header.Set("X-Kinga-Signature", "deadbeef")
	_, ok = Verify(req, []byte(body), keys)
	if ok {
		t.Errorf("expected verification to fail for invalid signature")
	}
}
