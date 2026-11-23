// load sends signed, unique assessment requests to a running local gateway and
// reports end-to-end latency. It is an experiment tool, not a production SLO test.
package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type result struct {
	latency time.Duration
	err     error
}

func main() {
	endpoint := flag.String("url", "http://127.0.0.1:8080/v1/assess", "gateway assessment URL")
	count := flag.Int("count", 100, "number of unique requests (maximum 300 per minute per tenant)")
	concurrency := flag.Int("concurrency", 10, "number of concurrent clients")
	tenant := flag.String("tenant", "spike", "tenant name")
	keyID := flag.String("key-id", "spike-key", "configured gateway API key ID")
	secret := flag.String("secret", "spike-secret-change-me", "configured gateway API secret")
	timeout := flag.Duration("timeout", 2*time.Second, "per-request timeout")
	flag.Parse()
	if *count < 1 || *count > 300 || *concurrency < 1 || *concurrency > *count || *secret == "" {
		fmt.Println("count must be 1..300, concurrency 1..count, and secret non-empty")
		os.Exit(2)
	}

	client := &http.Client{Timeout: *timeout}
	results := make(chan result, *count)
	var next atomic.Int64
	var workers sync.WaitGroup
	started := time.Now()
	for worker := 0; worker < *concurrency; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				index := int(next.Add(1))
				if index > *count {
					return
				}
				results <- send(client, *endpoint, *tenant, *keyID, *secret, index, *timeout)
			}
		}()
	}
	workers.Wait()
	close(results)

	latencies := make([]time.Duration, 0, *count)
	var failures int
	for item := range results {
		if item.err != nil {
			failures++
			fmt.Printf("request failed: %v\n", item.err)
			continue
		}
		latencies = append(latencies, item.latency)
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	elapsed := time.Since(started)
	fmt.Printf("requests=%d failures=%d elapsed=%s throughput=%.1f req/s\n", *count, failures, elapsed.Round(time.Millisecond), float64(len(latencies))/elapsed.Seconds())
	if len(latencies) > 0 {
		fmt.Printf("latency p50=%s p95=%s p99=%s max=%s\n", percentile(latencies, .50), percentile(latencies, .95), percentile(latencies, .99), latencies[len(latencies)-1])
	}
	if failures > 0 {
		os.Exit(1)
	}
}

func send(client *http.Client, endpoint, tenant, keyID, secret string, index int, timeout time.Duration) result {
	id := fmt.Sprintf("spike-%d-%06d", time.Now().UnixNano(), index)
	event := map[string]any{
		"tenant_event_id": id, "event_type": "funds_transfer", "occurred_at": time.Now().UTC().Format(time.RFC3339Nano),
		"channel": "api", "subject": map[string]string{"account_ref": "spike-account"},
		"transaction": map[string]any{"amount": 1000, "currency": "KES", "rail": "spike"},
	}
	body, err := json.Marshal(event)
	if err != nil {
		return result{err: err}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return result{err: err}
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp + "." + string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Kinga-Key", keyID)
	req.Header.Set("X-Kinga-Timestamp", timestamp)
	req.Header.Set("X-Kinga-Signature", hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set("X-Kinga-Tenant", tenant)
	req.Header.Set("Idempotency-Key", id)
	started := time.Now()
	response, err := client.Do(req)
	latency := time.Since(started)
	if err != nil {
		return result{err: err}
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode != http.StatusOK {
		return result{err: fmt.Errorf("gateway returned %d", response.StatusCode)}
	}
	return result{latency: latency}
}

func percentile(values []time.Duration, quantile float64) time.Duration {
	index := int(float64(len(values)-1) * quantile)
	return values[index]
}
