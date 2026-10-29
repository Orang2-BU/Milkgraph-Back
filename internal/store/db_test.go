package store

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestTransactionPersistence(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to isolated PostgreSQL")
	}
	ctx := context.Background()
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	id := t.Name()
	_, err = db.Update(ctx, "test", id, func(tx *Tx, current json.RawMessage) (Change, error) {
		return Change{Value: map[string]any{"ok": true}, Log: map[string]any{"kind": "test", "title": "test"}, Event: "test.changed"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := db.Get(ctx, "test", id)
	if err != nil || !json.Valid(body) {
		t.Fatalf("%s %v", body, err)
	}
	var count int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM milkgraph.audit WHERE body->>'title'='test'").Scan(&count); err != nil || count < 1 {
		t.Fatal(count, err)
	}
	if _, err = db.Pool.Exec(ctx, "UPDATE milkgraph.audit SET body=body WHERE body->>'title'='test'"); err == nil {
		t.Fatal("audit update allowed")
	}
}
