package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const queryTimeout = 10 * time.Second
const resetLock int64 = 728531790
const eventLock int64 = 728531791

type DB struct {
	Pool *pgxpool.Pool
}

type Tx struct {
	pgxTx pgx.Tx
}

type Change struct {
	Value   any
	Log     map[string]any
	Event   string
	EventID int64
}

type Event struct {
	ID   int64           `json:"id"`
	Kind string          `json:"kind"`
	At   string          `json:"at"`
	Data json.RawMessage `json:"data,omitempty"`
}

func Open(ctx context.Context, dsn string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}

	max := 20
	if raw, ok := os.LookupEnv("DATABASE_POOL_MAX"); ok {
		max, err = strconv.Atoi(raw)
		if err != nil || max < 1 || max > 100 {
			return nil, errors.New("DATABASE_POOL_MAX must be an integer from 1 to 100")
		}
	}
	cfg.MaxConns = int32(max)
	cfg.MinConns = 0
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "5000"
	cfg.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "15000"

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	db := &DB{Pool: pool}
	if err = pool.Ping(ctx); err == nil {
		err = db.migrate(ctx)
	}
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("initialize database: %w", err)
	}

	return db, nil
}

func (db *DB) Close() {
	if db.Pool != nil {
		db.Pool.Close()
	}
}

func (db *DB) migrate(ctx context.Context) error {
	return pgx.BeginTxFunc(ctx, db.Pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(728531792)"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
CREATE SCHEMA IF NOT EXISTS milkgraph;

CREATE TABLE IF NOT EXISTS milkgraph.state (
	entity text NOT NULL,
	id text NOT NULL,
	body jsonb NOT NULL,
	updated_at timestamptz NOT NULL DEFAULT now(),
	PRIMARY KEY (entity, id)
);
CREATE INDEX IF NOT EXISTS state_entity_updated ON milkgraph.state(entity, updated_at DESC, id);

CREATE TABLE IF NOT EXISTS milkgraph.audit (
	id bigserial PRIMARY KEY,
	kind text NOT NULL,
	title text NOT NULL,
	detail text NOT NULL DEFAULT '',
	note text NOT NULL DEFAULT '',
	by_user text NOT NULL,
	role text NOT NULL,
	at text NOT NULL,
	tone text NOT NULL DEFAULT 'neutral',
	body jsonb NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS audit_kind_id ON milkgraph.audit(kind, id DESC);

CREATE TABLE IF NOT EXISTS milkgraph.events (
	id bigserial PRIMARY KEY,
	kind text NOT NULL,
	at timestamptz NOT NULL DEFAULT clock_timestamp(),
	data jsonb
);

CREATE SEQUENCE IF NOT EXISTS milkgraph.hold_number;
CREATE TABLE IF NOT EXISTS milkgraph.audit_seeds (
 fingerprint text PRIMARY KEY
);

CREATE OR REPLACE FUNCTION milkgraph.prevent_audit_modification()
RETURNS TRIGGER AS $$
BEGIN
	RAISE EXCEPTION 'audit log is append-only and cannot be modified or deleted';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_prevent_audit_modification ON milkgraph.audit;
DROP TRIGGER IF EXISTS trg_prevent_audit_statement ON milkgraph.audit;

UPDATE milkgraph.audit SET body = body || jsonb_build_object('id', id)
WHERE body->'id' IS DISTINCT FROM to_jsonb(id);

CREATE TRIGGER trg_prevent_audit_statement
BEFORE UPDATE OR DELETE OR TRUNCATE ON milkgraph.audit
FOR EACH STATEMENT EXECUTE FUNCTION milkgraph.prevent_audit_modification();
`)
		return err
	})
}

func readState(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, entity, id string) (json.RawMessage, error) {
	var body []byte
	err := q.QueryRow(ctx, "SELECT body FROM milkgraph.state WHERE entity = $1 AND id = $2", entity, id).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return json.RawMessage(body), err
}

func (db *DB) Get(ctx context.Context, entity, id string) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	return readState(ctx, db.Pool, entity, id)
}

func (tx *Tx) lock(ctx context.Context, entity, id string) error {
	_, err := tx.pgxTx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", entity+"\x1f"+id)
	return err
}

func (tx *Tx) Get(ctx context.Context, entity, id string) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	if err := tx.lock(ctx, entity, id); err != nil {
		return nil, err
	}
	return readState(ctx, tx.pgxTx, entity, id)
}

func (tx *Tx) Put(ctx context.Context, entity, id string, value any) error {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	body, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal value: %w", err)
	}
	if err = tx.lock(ctx, entity, id); err != nil {
		return err
	}
	_, err = tx.pgxTx.Exec(ctx, `
		INSERT INTO milkgraph.state (entity, id, body, updated_at)
		VALUES ($1, $2, $3, clock_timestamp())
		ON CONFLICT (entity, id) DO UPDATE
		SET body = EXCLUDED.body, updated_at = EXCLUDED.updated_at
	`, entity, id, body)
	return err
}

func (tx *Tx) NextHold(ctx context.Context) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	var n int64
	err := tx.pgxTx.QueryRow(ctx, "SELECT nextval('milkgraph.hold_number')").Scan(&n)
	return n, err
}

func appendAudit(ctx context.Context, tx pgx.Tx, log map[string]any) (map[string]any, error) {
	copied := make(map[string]any, len(log)+1)
	for k, v := range log {
		copied[k] = v
	}
	var id int64
	if err := tx.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('milkgraph.audit', 'id'))").Scan(&id); err != nil {
		return nil, err
	}
	copied["id"] = id

	text := func(key string) string {
		v, _ := copied[key].(string)
		return v
	}
	if text("at") == "" {
		copied["at"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if text("tone") == "" {
		copied["tone"] = "neutral"
	}

	body, err := json.Marshal(copied)
	if err != nil {
		return nil, fmt.Errorf("marshal log: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO milkgraph.audit (id, kind, title, detail, note, by_user, role, at, tone, body)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, id, text("kind"), text("title"), text("detail"), text("note"), text("by"), text("role"), text("at"), text("tone"), body)
	return copied, err
}

func appendEvent(ctx context.Context, tx pgx.Tx, kind string, data any) (int64, error) {
	if strings.ContainsAny(kind, "\r\n") {
		return 0, errors.New("event kind cannot contain a newline")
	}
	body, err := json.Marshal(data)
	if err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", eventLock); err != nil {
		return 0, err
	}
	var id int64
	err = tx.QueryRow(ctx, "INSERT INTO milkgraph.events (kind, data) VALUES ($1, $2) RETURNING id", kind, body).Scan(&id)
	return id, err
}

func (db *DB) Update(ctx context.Context, entity, id string, fn func(*Tx, json.RawMessage) (Change, error)) (Change, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	var result Change
	err := pgx.BeginTxFunc(ctx, db.Pool, pgx.TxOptions{}, func(pgxTx pgx.Tx) error {
		if _, err := pgxTx.Exec(ctx, "SELECT pg_advisory_xact_lock_shared($1)", resetLock); err != nil {
			return err
		}
		tx := &Tx{pgxTx: pgxTx}
		current, err := tx.Get(ctx, entity, id)
		if err != nil {
			return err
		}

		change, err := fn(tx, current)
		if err != nil {
			return err
		}

		if err = tx.Put(ctx, entity, id, change.Value); err != nil {
			return err
		}

		if change.Log != nil {
			change.Log, err = appendAudit(ctx, pgxTx, change.Log)
			if err != nil {
				return err
			}
		}

		kind := change.Event
		if kind == "" {
			kind = entity + ".updated"
		}
		change.EventID, err = appendEvent(ctx, pgxTx, kind, map[string]any{
			"entity": entity,
			"id":     id,
			"value":  change.Value,
			"log":    change.Log,
		})
		if err != nil {
			return err
		}

		result = change
		return nil
	})

	if err != nil {
		return Change{}, err
	}
	return result, nil
}

func (db *DB) List(ctx context.Context, entity string) ([]json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	rows, err := db.Pool.Query(ctx, "SELECT body FROM milkgraph.state WHERE entity = $1 ORDER BY updated_at DESC, id", entity)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []json.RawMessage{}
	for rows.Next() {
		var raw json.RawMessage
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		result = append(result, raw)
	}
	return result, rows.Err()
}

func boundedLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}

func (db *DB) Audit(ctx context.Context, kind string, before int64, limit int) ([]json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	rows, err := db.Pool.Query(ctx, `
		SELECT body FROM milkgraph.audit
		WHERE ($1 = '' OR kind = $1)
		  AND ($2::bigint = 0 OR id < $2)
		ORDER BY id DESC
		LIMIT $3
	`, kind, before, boundedLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []json.RawMessage{}
	for rows.Next() {
		var raw json.RawMessage
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		result = append(result, raw)
	}
	return result, rows.Err()
}

func (db *DB) Events(ctx context.Context, after int64, limit int) ([]Event, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	rows, err := db.Pool.Query(ctx, "SELECT id, kind, at, data FROM milkgraph.events WHERE id > $1 ORDER BY id LIMIT $2", after, boundedLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []Event{}
	for rows.Next() {
		var e Event
		var at time.Time
		if err = rows.Scan(&e.ID, &e.Kind, &at, &e.Data); err != nil {
			return nil, err
		}
		e.At = at.UTC().Format(time.RFC3339Nano)
		result = append(result, e)
	}
	return result, rows.Err()
}

func (db *DB) LatestEventID(ctx context.Context) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	var id int64
	err := db.Pool.QueryRow(ctx, "SELECT COALESCE(max(id), 0) FROM milkgraph.events").Scan(&id)
	return id, err
}
