package api_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"milkgraph/internal/api"
	"milkgraph/internal/sse"
	"milkgraph/internal/store"
)

const networkUsers = 1000
const networkRamp = 5 * time.Second

func networkDB(t *testing.T) (*store.DB, string) {
	t.Helper()
	if os.Getenv("RUN_NETWORK_LOAD") != "1" || os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("set RUN_NETWORK_LOAD=1 and TEST_DATABASE_URL for disposable local PostgreSQL")
	}
	cfg, err := pgx.ParseConfig(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal("invalid TEST_DATABASE_URL (credentials omitted)")
	}
	if cfg.Host != "127.0.0.1" && cfg.Host != "localhost" && cfg.Host != "::1" {
		t.Fatal("network load tests require local disposable PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	admin, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal("connect local PostgreSQL failed (credentials omitted)")
	}
	name := fmt.Sprintf("milkgraph_load_%d_%d", os.Getpid(), time.Now().UnixNano())
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		admin.Close(context.Background())
		t.Fatal("CREATE DATABASE failed: use a disposable role with CREATEDB")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		defer admin.Close(ctx)
		if _, err := admin.Exec(ctx, "DROP DATABASE "+quoted+" WITH (FORCE)"); err != nil {
			t.Errorf("cleanup disposable database %s failed", name)
		}
	})
	dsn := (&url.URL{
		Scheme: "postgres", User: url.UserPassword(cfg.User, cfg.Password),
		Host: net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port))), Path: "/" + name,
		RawQuery: "sslmode=disable",
	}).String()
	db, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal("initialize disposable database failed (credentials omitted)")
	}
	t.Cleanup(db.Close)
	var actual string
	if err := db.Pool.QueryRow(ctx, "SELECT current_database()").Scan(&actual); err != nil || actual != name {
		t.Fatal("load database isolation failed")
	}
	return db, dsn
}

func networkServer(t *testing.T, db *store.DB) *httptest.Server {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	hub := sse.New(db)
	srv := &api.Server{Mode: "demo", DB: db, Hub: hub}
	if err := srv.Bootstrap(ctx); err != nil {
		cancel()
		hub.Close()
		t.Fatal(err)
	}
	hub.Run(ctx)
	server := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		cancel()
		hub.Close()
		server.CloseClientConnections()
		server.Close()
	})
	return server
}

func networkClient(t *testing.T) *http.Client {
	t.Helper()
	transport := &http.Transport{
		Proxy:           nil,
		DialContext:     (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxConnsPerHost: 2000, MaxIdleConns: 2000, MaxIdleConnsPerHost: 2000,
		IdleConnTimeout: 30 * time.Second, ResponseHeaderTimeout: 30 * time.Second,
	}
	t.Cleanup(transport.CloseIdleConnections)
	// SSE uses request cancellation, never a whole-stream client timeout.
	return &http.Client{Transport: transport}
}

type networkEndpoint struct{ method, path, role, body string }

func networkRequest(client *http.Client, base string, ep networkEndpoint) (int, []byte, time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	started := time.Now()
	req, err := http.NewRequestWithContext(ctx, ep.method, base+"/api/v1"+ep.path, strings.NewReader(ep.body))
	if err != nil {
		return 0, nil, time.Since(started), err
	}
	req.Header.Set("X-Demo-Role", ep.role)
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return 0, nil, time.Since(started), err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if err == nil && (len(body) > 2<<20 || !json.Valid(body)) {
		err = fmt.Errorf("oversized or invalid JSON response")
	}
	return res.StatusCode, body, time.Since(started), err
}

func networkSource(t *testing.T) string {
	t.Helper()
	seeds, err := api.LoadAllSeeds()
	if err != nil {
		t.Fatal(err)
	}
	return seeds.Ops["dataSources"].([]any)[0].(map[string]any)["id"].(string)
}

func networkSync(t *testing.T, db *store.DB, client *http.Client, base, source string) store.Event {
	t.Helper()
	before, err := db.LatestEventID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	status, body, _, err := networkRequest(client, base, networkEndpoint{"POST", "/data-sources/" + source + "/sync", "planner", `{}`})
	if err != nil || status != 200 {
		t.Fatalf("source sync status=%d error=%v body=%.200s", status, err, body)
	}
	events, err := db.Events(context.Background(), before, 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("HTTP source sync must commit exactly one durable PostgreSQL event: events=%d error=%v", len(events), err)
	}
	var data struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(events[0].Data, &data) != nil || data.ID != source || events[0].Kind == "" {
		t.Fatalf("source event lost identity: %+v", events[0])
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, events[0].Data); err != nil {
		t.Fatal(err)
	}
	events[0].Data = compact.Bytes()
	return events[0]
}

func TestNetworkSourceSyncIsDurable(t *testing.T) {
	db, _ := networkDB(t)
	server := networkServer(t, db)
	networkSync(t, db, networkClient(t), server.URL, networkSource(t))
}

func networkDataset(t *testing.T, db *store.DB, label string) (state, audit, events int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	err := db.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM milkgraph.state), (SELECT count(*) FROM milkgraph.audit), (SELECT count(*) FROM milkgraph.events)`).Scan(&state, &audit, &events)
	if err != nil {
		t.Fatal(err)
	}
	seeds, err := api.LoadAllSeeds()
	if err != nil {
		t.Fatal(err)
	}
	graph := seeds.Scenario["graph"].(map[string]any)
	t.Logf("dataset=%s graph_nodes=%d graph_edges=%d state_rows=%d audit_rows=%d event_rows=%d db_pool_max=%d", label, len(graph["nodes"].([]any)), len(graph["edges"].([]any)), state, audit, events, db.Pool.Config().MaxConns)
	return
}

func networkHardware(t *testing.T) {
	t.Helper()
	cpu := os.Getenv("PROCESSOR_IDENTIFIER")
	if cpu == "" {
		if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
			for _, line := range strings.Split(string(b), "\n") {
				if strings.HasPrefix(line, "model name") {
					cpu = strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
					break
				}
			}
		}
	}
	t.Logf("hardware os=%s arch=%s go=%s logical_cpus=%d gomaxprocs=%d cpu=%q", runtime.GOOS, runtime.GOARCH, runtime.Version(), runtime.NumCPU(), runtime.GOMAXPROCS(0), cpu)
}

func networkPercentile(sorted []time.Duration, percentile int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	return sorted[(len(sorted)*percentile+99)/100-1]
}

func TestNetworkLoad1000Users(t *testing.T) {
	db, _ := networkDB(t)
	duration := 30 * time.Second
	if raw := os.Getenv("NETWORK_LOAD_DURATION"); raw != "" {
		var err error
		duration, err = time.ParseDuration(raw)
		if err != nil || duration < 20*time.Second || duration > 5*time.Minute {
			t.Fatal("NETWORK_LOAD_DURATION must be between 20s and 5m")
		}
	}
	server := networkServer(t, db)
	client := networkClient(t)
	source := networkSource(t)
	networkHardware(t)
	_, auditBefore, eventsBefore := networkDataset(t, db, "before")
	endpoints := []networkEndpoint{
		{"GET", "/graph", "planner", ""},
		{"GET", "/receiving", "receiving", ""},
		{"GET", "/lab/results", "qa", ""},
		{"GET", "/holds", "qa", ""},
		{"GET", "/audit?limit=100", "planner", ""},
		{"GET", "/trace/backward?lotId=FG-1013-07", "qa", ""},
		{"GET", "/trace/forward?rtId=RT-04&date=2026-10-13", "qa", ""},
		{"GET", "/limits", "planner", ""},
		{"GET", "/plans/current", "planner", ""},
		{"POST", "/plans/current/check", "planner", `{}`},
		{"GET", "/supply/demand-plan", "planner", ""},
		{"POST", "/data-sources/" + source + "/sync", "planner", `{}`},
	}
	approval := networkEndpoint{"POST", "/plans/current/decision", "planner", `{"status":"approved","note":"network load contention"}`}
	type result struct {
		latencies                                         []time.Duration
		mix, statuses                                     map[string]int
		errors                                            []string
		unexpected, conflicts, approved, syncs, sustained int
	}
	results := make(chan result, networkUsers)
	barrier := make(chan struct{})
	var ready sync.WaitGroup
	ready.Add(networkUsers)
	var writes atomic.Int64
	started := time.Now()
	steadyAt := started.Add(networkRamp)
	until := steadyAt.Add(duration)
	for u := 0; u < networkUsers; u++ {
		go func(user int) {
			ready.Done()
			<-barrier
			time.Sleep(time.Duration(user) * networkRamp / networkUsers)
			rng := rand.New(rand.NewSource(int64(user + 719)))
			r := result{mix: map[string]int{}, statuses: map[string]int{}}
			approvedOnce := false
			for time.Now().Before(until) {
				steady := !time.Now().Before(steadyAt)
				ep := endpoints[rng.Intn(len(endpoints))]
				isApproval := steady && !approvedOnce
				if isApproval {
					ep, approvedOnce = approval, true
				} else if ep.method == "POST" && strings.HasSuffix(ep.path, "/sync") && writes.Add(1) > 500 {
					ep = networkEndpoint{"GET", "/data-sources", "planner", ""}
				}
				status, body, latency, err := networkRequest(client, server.URL, ep)
				r.latencies = append(r.latencies, latency)
				r.mix[ep.method+" "+ep.path]++
				r.statuses[strconv.Itoa(status)]++
				if steady {
					r.sustained++
				}
				expected := err == nil && status == 200
				if isApproval && err == nil && status == 409 {
					var response struct {
						Error struct {
							Code string `json:"code"`
						} `json:"error"`
					}
					expected = json.Unmarshal(body, &response) == nil && response.Error.Code == "CONFLICT"
					if expected {
						r.conflicts++
					}
				}
				if isApproval && status == 200 && err == nil {
					r.approved++
				}
				if ep.method == "POST" && strings.HasSuffix(ep.path, "/sync") && expected {
					r.syncs++
				}
				if !expected {
					r.unexpected++
					if len(r.errors) < 2 {
						r.errors = append(r.errors, fmt.Sprintf("%s %s status=%d error=%v body=%.160s", ep.method, ep.path, status, err, body))
					}
				}
				time.Sleep(time.Duration(50+rng.Intn(151)) * time.Millisecond)
			}
			results <- r
		}(u)
	}
	ready.Wait()
	close(barrier)
	total := result{mix: map[string]int{}, statuses: map[string]int{}}
	for u := 0; u < networkUsers; u++ {
		r := <-results
		total.latencies = append(total.latencies, r.latencies...)
		for k, v := range r.mix {
			total.mix[k] += v
		}
		for k, v := range r.statuses {
			total.statuses[k] += v
		}
		total.unexpected += r.unexpected
		total.conflicts += r.conflicts
		total.approved += r.approved
		total.syncs += r.syncs
		total.sustained += r.sustained
		for _, err := range r.errors {
			if len(total.errors) < 10 {
				total.errors = append(total.errors, err)
			}
		}
	}
	elapsed := time.Since(started)
	sort.Slice(total.latencies, func(i, j int) bool { return total.latencies[i] < total.latencies[j] })
	p95 := networkPercentile(total.latencies, 95)
	t.Logf("REST users=%d ramp=%s sustained=%s elapsed=%s think=50-200ms requests=%d sustained_requests=%d throughput=%.2f_req/s unexpected=%d error_rate=%.6f expected409=%d approval200=%d source_sync200=%d", networkUsers, networkRamp, duration, elapsed, len(total.latencies), total.sustained, float64(len(total.latencies))/elapsed.Seconds(), total.unexpected, float64(total.unexpected)/float64(len(total.latencies)), total.conflicts, total.approved, total.syncs)
	t.Logf("REST latency p50=%s p95=%s p99=%s max=%s", networkPercentile(total.latencies, 50), p95, networkPercentile(total.latencies, 99), networkPercentile(total.latencies, 100))
	t.Logf("REST status_counts=%v", total.statuses)
	keys := make([]string, 0, len(total.mix))
	for k := range total.mix {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Logf("mix %s count=%d", k, total.mix[k])
	}
	for _, err := range total.errors {
		t.Log(err)
	}
	_, auditAfter, eventsAfter := networkDataset(t, db, "after")
	if total.unexpected != 0 || total.approved != 1 || total.conflicts != networkUsers-1 {
		t.Errorf("unexpected=%d approval200=%d expected409=%d; require 0/1/999", total.unexpected, total.approved, total.conflicts)
	}
	if total.syncs == 0 || eventsAfter-eventsBefore != int64(total.syncs+total.approved) || auditAfter <= auditBefore {
		t.Errorf("writes not durable: syncs=%d event_delta=%d audit_delta=%d", total.syncs, eventsAfter-eventsBefore, auditAfter-auditBefore)
	}
	if auditAfter >= 100000 {
		t.Error("audit dataset exceeded bounded test size")
	}
	if p95 >= 5*time.Second {
		t.Errorf("local regression ceiling: p95=%s must be <5s", p95)
	}
	if total.sustained < networkUsers {
		t.Errorf("insufficient sustained requests=%d", total.sustained)
	}
}

type networkFrame struct {
	id    int64
	kind  string
	event store.Event
}

func networkReadFrame(reader *bufio.Reader) (networkFrame, error) {
	var frame networkFrame
	var data []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return frame, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			if len(data) == 0 {
				continue
			}
			if err := json.Unmarshal([]byte(strings.Join(data, "\n")), &frame.event); err != nil {
				return frame, err
			}
			return frame, nil
		}
		switch {
		case strings.HasPrefix(line, "id:"):
			frame.id, err = strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "id:")), 10, 64)
			if err != nil {
				return frame, err
			}
		case strings.HasPrefix(line, "event:"):
			frame.kind = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
}

func TestNetworkSSE1000Connections(t *testing.T) {
	db, dsn := networkDB(t)
	other, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal("open independent DB pool failed (credentials omitted)")
	}
	t.Cleanup(other.Close)
	server := networkServer(t, db)
	writer := networkServer(t, other)
	client := networkClient(t)
	source := networkSource(t)
	baseline := networkSync(t, other, client, writer.URL, source)
	networkHardware(t)
	networkDataset(t, db, "sse_before")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	type received struct {
		frame   networkFrame
		latency time.Duration
		err     error
	}
	opened := make(chan error, networkUsers)
	messages := make(chan received, networkUsers)
	barrier, release := make(chan struct{}), make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(networkUsers)
	started := time.Now()
	var mutationAt atomic.Int64
	for i := 0; i < networkUsers; i++ {
		go func(idx int) {
			defer workers.Done()
			<-barrier
			time.Sleep(time.Duration(idx) * 2 * time.Millisecond)
			req, err := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/v1/events", nil)
			if err != nil {
				opened <- err
				return
			}
			req.Header.Set("X-Demo-Role", "planner")
			res, err := client.Do(req)
			if err != nil {
				opened <- err
				return
			}
			defer res.Body.Close()
			if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
				opened <- fmt.Errorf("SSE status=%d type=%q", res.StatusCode, res.Header.Get("Content-Type"))
				return
			}
			opened <- nil
			frame, err := networkReadFrame(bufio.NewReader(res.Body))
			messages <- received{frame, time.Since(time.Unix(0, mutationAt.Load())), err}
			select {
			case <-release:
			case <-ctx.Done():
			}
		}(i)
	}
	close(barrier)
	openErrors := 0
	for i := 0; i < networkUsers; i++ {
		select {
		case err := <-opened:
			if err != nil {
				openErrors++
				if openErrors <= 3 {
					t.Log(err)
				}
			}
		case <-ctx.Done():
			t.Fatal("timeout opening 1000 SSE connections")
		}
	}
	if openErrors != 0 {
		t.Fatalf("SSE connection errors=%d", openErrors)
	}
	openDuration := time.Since(started)
	mutationAt.Store(time.Now().UnixNano())
	expected := networkSync(t, other, client, writer.URL, source)
	latencies := make([]time.Duration, 0, networkUsers)
	for i := 0; i < networkUsers; i++ {
		select {
		case got := <-messages:
			if got.err != nil || got.frame.id != expected.ID || got.frame.kind != expected.Kind || got.frame.event.ID != expected.ID || got.frame.event.Kind != expected.Kind || !bytes.Equal(got.frame.event.Data, expected.Data) {
				t.Fatalf("SSE client=%d mismatched: got_data=%s expected_data=%s", i, string(got.frame.event.Data), string(expected.Data))
			}
			latencies = append(latencies, got.latency)
		case <-ctx.Done():
			t.Fatalf("SSE delivered=%d/%d before timeout", i, networkUsers)
		}
	}
	// Reconnect to instance A using the cursor preceding the write on independent instance B.
	replayCtx, replayCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer replayCancel()
	req, err := http.NewRequestWithContext(replayCtx, "GET", server.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Demo-Role", "planner")
	req.Header.Set("Last-Event-ID", strconv.FormatInt(baseline.ID, 10))
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		res.Body.Close()
		t.Fatalf("replay status=%d", res.StatusCode)
	}
	frame, err := networkReadFrame(bufio.NewReader(res.Body))
	res.Body.Close()
	replayCancel()
	if err != nil || frame.id != expected.ID || frame.kind != expected.Kind || !bytes.Equal(frame.event.Data, expected.Data) {
		t.Fatalf("Last-Event-ID replay mismatch: frame=%+v error=%v", frame, err)
	}
	shutdownAt := time.Now()
	cancel()
	close(release)
	done := make(chan struct{})
	go func() { workers.Wait(); server.Close(); writer.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("SSE cancellation/server shutdown exceeded 5s")
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	t.Logf("SSE simultaneous_connections=%d delivered=%d open_duration=%s delivery_p50=%s delivery_p95=%s delivery_p99=%s delivery_max=%s replay=passed shutdown=%s elapsed=%s", networkUsers, len(latencies), openDuration, networkPercentile(latencies, 50), networkPercentile(latencies, 95), networkPercentile(latencies, 99), networkPercentile(latencies, 100), time.Since(shutdownAt), time.Since(started))
	networkDataset(t, db, "sse_after")
}
