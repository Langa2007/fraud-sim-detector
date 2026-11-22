package main

import (
	"net/http"
	"os"
	"time"
)

func main() {
	endpoint := os.Getenv("KINGA_HEALTHCHECK_URL")
	if endpoint == "" {
		endpoint = "http://127.0.0.1:8080/readyz"
	}
	client := http.Client{Timeout: 1500 * time.Millisecond}
	response, err := client.Get(endpoint)
	if err != nil {
		os.Exit(1)
	}
	_ = response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		os.Exit(1)
	}
}
