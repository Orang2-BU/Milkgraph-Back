package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testDB(t *testing.T) *DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to isolated PostgreSQL")
	}
	db, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

func TestDurableWrite(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	id := fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())
	before, err := db.LatestEventID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	change, err := db.Update(ctx, "test", id, func(tx *Tx, current json.RawMessage) (Change, error) {
		if err := tx.Put(ctx, "test", id+"-side", map[string]bool{"side": true}); err != nil {
			return Change{}, err
		}
		side, err := tx.Get(ctx, "test", id+"-side")
		if err != nil || len(side) == 0 {
			return Change{}, fmt.Errorf("read side: %s %v", side, err)
		}
		_, err = tx.NextHold(ctx)
		if err != nil {
			return Change{}, err
		}
		return Change{Value: map[string]bool{"ok": true}, Log: map[string]any{"kind": id, "title": id}, Event: "test.created"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if change.EventID <= before {
		t.Fatalf("event id: %d", change.EventID)
	}
	logs, err := db.Audit(ctx, id, 0, 10)
	if err != nil || len(logs) != 1 {
		t.Fatalf("logs %s %v", logs, err)
	}
	var log map[string]any
	json.Unmarshal(logs[0], &log)
	if log["id"] != float64(change.Log["id"].(int64)) {
		t.Fatalf("persisted audit id: %s vs %v", logs[0], change.Log)
	}
	events, err := db.Events(ctx, before, 100)
	if err != nil || len(events) != 1 || events[0].ID != change.EventID || events[0].Kind != "test.created" || !json.Valid(events[0].Data) {
		t.Fatalf("events: %+v %v", events, err)
	}
	values, err := db.List(ctx, "test")
	if err != nil || len(values) < 2 {
		t.Fatalf("list: %s %v", values, err)
	}
	db2 := testDB(t)
	raw, err := db2.Get(ctx, "test", id)
	if err != nil || !json.Valid(raw) {
		t.Fatalf("persistence %s %v", raw, err)
	}
	_, err = db.Update(ctx, "test", id, func(tx *Tx, current json.RawMessage) (Change, error) {
		return Change{Value: current, Event: "test.no-log"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRollback(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	id := fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())
	before, _ := db.LatestEventID(ctx)
	_, err := db.Update(ctx, "test", id, func(tx *Tx, current json.RawMessage) (Change, error) {
		if err := tx.Put(ctx, "test", id+"-side", true); err != nil {
			return Change{}, err
		}
		return Change{Value: true, Log: map[string]any{"kind": id, "title": id}, Event: "invalid\nname"}, nil
	})
	if err == nil {
		t.Fatal("invalid event accepted")
	}
	for _, key := range []string{id, id + "-side"} {
		raw, err := db.Get(ctx, "test", key)
		if err != nil || raw != nil {
			t.Fatalf("rollback state %s %v", raw, err)
		}
	}
	logs, err := db.Audit(ctx, id, 0, 10)
	if err != nil || len(logs) != 0 {
		t.Fatalf("rollback audit %s %v", logs, err)
	}
	after, _ := db.LatestEventID(ctx)
	if after != before {
		t.Fatalf("rollback event %d != %d", after, before)
	}
}

func TestAppendOnlyStatements(t *testing.T) {
	db := testDB(t)
	for _, sql := range []string{"UPDATE milkgraph.audit SET body=body WHERE false", "DELETE FROM milkgraph.audit WHERE false", "TRUNCATE milkgraph.audit"} {
		if _, err := db.Pool.Exec(context.Background(), sql); err == nil {
			t.Fatalf("allowed %s", sql)
		}
	}
}

func TestSeedAuditReset(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	id := fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())
	seed := []json.RawMessage{json.RawMessage(fmt.Sprintf(`{"id":"historical","kind":%q,"title":"historical","at":"2025-01-01"}`, id))}
	if err := db.SeedAudit(ctx, seed); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedAudit(ctx, seed); err != nil {
		t.Fatal(err)
	}
	logs, err := db.Audit(ctx, id, 0, 10)
	if err != nil || len(logs) != 1 {
		t.Fatalf("seed %s %v", logs, err)
	}
	_, err = db.Update(ctx, "test", id, func(tx *Tx, current json.RawMessage) (Change, error) { return Change{Value: true}, nil })
	if err != nil {
		t.Fatal(err)
	}
	before, _ := db.LatestEventID(ctx)
	if err = db.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	raw, err := db.Get(ctx, "test", id)
	if err != nil || raw != nil {
		t.Fatalf("reset state %s %v", raw, err)
	}
	logs, err = db.Audit(ctx, id, 0, 10)
	if err != nil || len(logs) != 1 {
		t.Fatalf("reset audit %s %v", logs, err)
	}
	events, err := db.Events(ctx, before, 10)
	if err != nil || len(events) != 1 || events[0].Kind != "demo.reset" {
		t.Fatalf("reset event %+v %v", events, err)
	}
}

func TestPoolBounds(t *testing.T) {
	for _, value := range []string{"0", "101", "x", "-1", ""} {
		t.Run("invalid_"+value, func(t *testing.T) {
			t.Setenv("DATABASE_POOL_MAX", value)
			if db, err := Open(context.Background(), "postgres://localhost/test"); err == nil {
				db.Close()
				t.Fatal("accepted invalid pool maximum")
			}
		})
	}
}

func TestConcurrentMissingRow(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	id := fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())
	start := make(chan struct{})
	var success atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := db.Update(ctx, "test", id, func(tx *Tx, current json.RawMessage) (Change, error) {
				if len(current) > 0 {
					return Change{}, errors.New("duplicate")
				}
				time.Sleep(10 * time.Millisecond)
				return Change{Value: map[string]bool{"ok": true}, Log: map[string]any{"kind": "test", "title": id}, Event: "test.created"}, nil
			})
			if err == nil {
				success.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if success.Load() != 1 {
		t.Fatalf("successful duplicate writes = %d, want 1", success.Load())
	}
}
