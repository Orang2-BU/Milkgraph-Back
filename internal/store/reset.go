package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

func (db *DB) SeedAudit(ctx context.Context, logs []json.RawMessage) error {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	return pgx.BeginTxFunc(ctx, db.Pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock_shared($1)", resetLock); err != nil {
			return err
		}
		for _, raw := range logs {
			var log map[string]any
			if err := json.Unmarshal(raw, &log); err != nil {
				return err
			}
			if log == nil {
				return errors.New("audit seed must be an object")
			}
			canonical, err := json.Marshal(log)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(canonical)
			tag, err := tx.Exec(ctx, "INSERT INTO milkgraph.audit_seeds(fingerprint) VALUES($1) ON CONFLICT DO NOTHING", hex.EncodeToString(sum[:]))
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				continue
			}
			if _, err = appendAudit(ctx, tx, log); err != nil {
				return err
			}
		}
		return nil
	})
}

func (db *DB) Reset(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	return pgx.BeginTxFunc(ctx, db.Pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", resetLock); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "DELETE FROM milkgraph.state"); err != nil {
			return err
		}
		_, err := appendEvent(ctx, tx, "demo.reset", map[string]any{"reset": true})
		return err
	})
}
