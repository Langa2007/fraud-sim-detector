package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
)

type Credential struct {
	Tenant string
	Secret []byte
}

func ParseKeys(raw string) map[string]Credential {
	out := map[string]Credential{}
	for _, part := range strings.Split(raw, ",") {
		pair := strings.SplitN(strings.TrimSpace(part), ":", 3)
		if len(pair) == 3 && pair[0] != "" && pair[1] != "" && pair[2] != "" {
			out[pair[1]] = Credential{Tenant: pair[0], Secret: []byte(pair[2])}
		}
	}
	return out
}
func Sign(secret []byte, timestamp, body string) string {
	m := hmac.New(sha256.New, secret)
	_, _ = m.Write([]byte(timestamp + "." + body))
	return hex.EncodeToString(m.Sum(nil))
}
func Verify(r *http.Request, body []byte, keys map[string]Credential) (Credential, bool) {
	key := r.Header.Get("X-Kinga-Key")
	c, ok := keys[key]
	if !ok {
		return Credential{}, false
	}
	ts := r.Header.Get("X-Kinga-Timestamp")
	sig := r.Header.Get("X-Kinga-Signature")
	if ts == "" || sig == "" {
		return Credential{}, false
	}
	expected, err := hex.DecodeString(Sign(c.Secret, ts, string(body)))
	if err != nil {
		return Credential{}, false
	}
	got, err := hex.DecodeString(sig)
	if err != nil {
		return Credential{}, false
	}
	return c, hmac.Equal(expected, got)
}
