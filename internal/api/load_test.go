package api_test

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"milkgraph/internal/api"
)

// TestConcurrency1000VirtualUsers exercises the REST server with 1,000 concurrent virtual users
// performing a realistic mix of read and computation requests.
// Measures: throughput, error rate, p50/p95/p99 latencies.
func TestConcurrency1000VirtualUsers(t *testing.T) {
	srv := &api.Server{Mode: "demo"}
	handler := srv.Handler()

	const numUsers = 1000
	const requestsPerUser = 5
	const totalExpected = numUsers * requestsPerUser

	endpoints := []struct {
		method string
		path   string
		role   string
	}{
		{"GET", "/api/v1/meta", "planner"},
		{"GET", "/api/v1/session", "planner"},
		{"GET", "/api/v1/receiving", "receiving"},
		{"GET", "/api/v1/graph", "planner"},
		{"GET", "/api/v1/plans/current", "supervisor"},
		{"POST", "/api/v1/plans/current/check", "planner"},
		{"GET", "/api/v1/trace/backward?lotId=FG-1013-07", "qa"},
		{"GET", "/api/v1/trace/forward?rtId=RT-04&date=2026-10-13", "qa"},
		{"GET", "/api/v1/limits", "planner"},
		{"GET", "/api/v1/audit", "planner"},
	}

	latencies := make([]time.Duration, totalExpected)
	var latIndex atomic.Int64
	var successCount atomic.Int64
	var errorCount atomic.Int64

	startBarrier := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(numUsers)

	overallStart := time.Now()

	for u := 0; u < numUsers; u++ {
		go func(userId int) {
			defer wg.Done()
			<-startBarrier // synchronize start of all 1000 goroutines

			r := rand.New(rand.NewSource(int64(userId + 42)))
			for req := 0; req < requestsPerUser; req++ {
				ep := endpoints[r.Intn(len(endpoints))]
				httpReq := httptest.NewRequest(ep.method, ep.path, nil)
				httpReq.Header.Set("X-Demo-Role", ep.role)
				rec := httptest.NewRecorder()

				t0 := time.Now()
				handler.ServeHTTP(rec, httpReq)
				dur := time.Since(t0)

				idx := latIndex.Add(1) - 1
				if idx < int64(totalExpected) {
					latencies[idx] = dur
				}

				if rec.Code == http.StatusOK && json.Valid(rec.Body.Bytes()) {
					successCount.Add(1)
				} else {
					errorCount.Add(1)
				}
			}
		}(u)
	}

	close(startBarrier)
	wg.Wait()
	overallDur := time.Since(overallStart)

	totalRequests := successCount.Load() + errorCount.Load()
	if totalRequests == 0 {
		t.Fatal("no requests completed")
	}

	validLats := latencies[:latIndex.Load()]
	sort.Slice(validLats, func(i, j int) bool { return validLats[i] < validLats[j] })

	p50 := validLats[len(validLats)*50/100]
	p95 := validLats[len(validLats)*95/100]
	p99 := validLats[len(validLats)*99/100]
	maxLat := validLats[len(validLats)-1]
	rps := float64(totalRequests) / overallDur.Seconds()

	t.Logf("=== Load Test Report: 1000 Concurrent Virtual Users ===")
	t.Logf("Total Requests:  %d", totalRequests)
	t.Logf("Success:         %d (%.2f%%)", successCount.Load(), float64(successCount.Load())*100/float64(totalRequests))
	t.Logf("Errors:          %d (%.2f%%)", errorCount.Load(), float64(errorCount.Load())*100/float64(totalRequests))
	t.Logf("Total Duration:  %v", overallDur)
	t.Logf("Throughput:      %.1f req/s", rps)
	t.Logf("Latency p50:     %v", p50)
	t.Logf("Latency p95:     %v", p95)
	t.Logf("Latency p99:     %v", p99)
	t.Logf("Latency max:     %v", maxLat)

	if errorCount.Load() > 0 {
		t.Fatalf("expected 0 errors under concurrency, got %d", errorCount.Load())
	}
}
