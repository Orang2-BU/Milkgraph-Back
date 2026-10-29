package sse

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"milkgraph/internal/store"
)

func openDB(t *testing.T) *store.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to isolated PostgreSQL")
	}
	db, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}
func writeEvent(t *testing.T, db *store.DB, kind string) int64 {
	t.Helper()
	c, err := db.Update(context.Background(), "sse-test", fmt.Sprint(time.Now().UnixNano()), func(tx *store.Tx, raw json.RawMessage) (store.Change, error) {
		return store.Change{Value: true, Event: kind}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return c.EventID
}
func readFrame(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	var b strings.Builder
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		b.WriteString(line)
		if line == "\n" {
			return b.String()
		}
	}
}
func TestReplayAndCrossReplicaLive(t *testing.T) {
	db := openDB(t)
	db2 := openDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hub := New(db)
	hub.Run(ctx)
	hub.Run(ctx)
	defer hub.Close()
	before, err := db.LatestEventID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	replayID := writeEvent(t, db2, "test.replay")
	server := httptest.NewServer(hub)
	defer server.Close()
	req, _ := http.NewRequest("GET", server.URL, nil)
	req.Header.Set("Last-Event-ID", fmt.Sprint(before))
	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("unexpected CORS")
	}
	reader := bufio.NewReader(resp.Body)
	frame := readFrame(t, reader)
	if !strings.Contains(frame, fmt.Sprintf("id: %d", replayID)) || !strings.Contains(frame, "event: test.replay") {
		t.Fatalf("replay frame %q", frame)
	}
	liveID := writeEvent(t, db2, "test.live")
	frame = readFrame(t, reader)
	if !strings.Contains(frame, fmt.Sprintf("id: %d", liveID)) || !strings.Contains(frame, "event: test.live") {
		t.Fatalf("live frame %q", frame)
	}
}
func TestSlowClientDisconnected(t *testing.T) {
	hub := New(nil)
	defer hub.Close()
	client := hub.subscribe()
	for i := 0; i <= clientBuffer; i++ {
		hub.publish(store.Event{ID: int64(i + 1), Kind: "test"})
	}
	select {
	case <-client.done:
	default:
		t.Fatal("slow client remained connected")
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if len(hub.clients) != 0 {
		t.Fatal("slow client leaked")
	}
}

func TestReplayLimit(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	hub := New(db)
	defer hub.Close()
	before, err := db.LatestEventID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Insert an isolated, committed history batch without 1001 round trips.
	_, err = db.Pool.Exec(ctx, "INSERT INTO milkgraph.events(kind,data) SELECT 'test.bulk', '{}'::jsonb FROM generate_series(1,1001)")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Last-Event-ID", fmt.Sprint(before))
	w := httptest.NewRecorder()
	hub.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("unbounded replay: %d", w.Code)
	}
}

func TestInvalidCursor(t *testing.T) {
	hub := New(openDB(t))
	defer hub.Close()
	for _, id := range []string{"-1", "garbage", "9223372036854775808"} {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Last-Event-ID", id)
		w := httptest.NewRecorder()
		hub.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("cursor %q: %d", id, w.Code)
		}
	}
}
