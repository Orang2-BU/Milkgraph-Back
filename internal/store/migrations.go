package store

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

var migrationSQL fs.FS

func init() {
	sub, err := fs.Sub(embeddedMigrations, "migrations")
	if err != nil {
		panic(err)
	}
	migrationSQL = sub
}

const migrationLock int64 = 728531792

type migration struct {
	version  int
	name     string
	checksum string
	sql      string
}

func readMigrations(fsys fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var ms []migration
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			return nil, fmt.Errorf("unexpected non-sql file in migrations: %s", name)
		}
		base := strings.TrimSuffix(name, ".sql")
		parts := strings.SplitN(base, "_", 2)
		if len(parts) != 2 || len(parts[0]) != 4 || parts[1] == "" {
			return nil, fmt.Errorf("malformed migration filename: %s", name)
		}
		v, err := strconv.Atoi(parts[0])
		if err != nil || v <= 0 {
			return nil, fmt.Errorf("invalid migration version in %s: %w", name, err)
		}
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		ms = append(ms, migration{
			version:  v,
			name:     parts[1],
			checksum: hex.EncodeToString(sum[:]),
			sql:      string(data),
		})
	}
	if len(ms) == 0 {
		return nil, errors.New("no migrations found")
	}
	sort.Slice(ms, func(i, j int) bool {
		return ms[i].version < ms[j].version
	})
	for i, m := range ms {
		expected := i + 1
		if m.version != expected {
			return nil, fmt.Errorf("migration sequence gap or duplicate: expected version %d, got %d", expected, m.version)
		}
	}
	return ms, nil
}

func verifyLegacyBaseline(ctx context.Context, tx pgx.Tx) error {
	// 1. Verify tables
	rows, err := tx.Query(ctx, "SELECT tablename FROM pg_tables WHERE schemaname = 'milkgraph' ORDER BY tablename")
	if err != nil {
		return err
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return err
		}
		tables = append(tables, t)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	expectedTables := []string{"audit", "audit_seeds", "events", "state"}
	if len(tables) != len(expectedTables) {
		return fmt.Errorf("table count mismatch: got %v, want %v", tables, expectedTables)
	}
	for i, t := range tables {
		if t != expectedTables[i] {
			return fmt.Errorf("unexpected table: got %s, want %s", t, expectedTables[i])
		}
	}

	// 2. Verify columns and types
	colRows, err := tx.Query(ctx, `
		SELECT table_name, column_name, data_type, is_nullable
		FROM information_schema.columns
		WHERE table_schema = 'milkgraph'
		ORDER BY table_name, column_name
	`)
	if err != nil {
		return err
	}
	defer colRows.Close()
	gotCols := make(map[string]string)
	for colRows.Next() {
		var tbl, col, dt, null string
		if err := colRows.Scan(&tbl, &col, &dt, &null); err != nil {
			return err
		}
		gotCols[tbl+"."+col] = dt + "|" + null
	}
	if err := colRows.Err(); err != nil {
		return err
	}
	colRows.Close()

	expectedCols := map[string]string{
		"audit.at":                "text|NO",
		"audit.body":              "jsonb|NO",
		"audit.by_user":           "text|NO",
		"audit.created_at":        "timestamp with time zone|NO",
		"audit.detail":            "text|NO",
		"audit.id":                "bigint|NO",
		"audit.kind":              "text|NO",
		"audit.note":              "text|NO",
		"audit.role":              "text|NO",
		"audit.title":             "text|NO",
		"audit.tone":              "text|NO",
		"audit_seeds.fingerprint": "text|NO",
		"events.at":               "timestamp with time zone|NO",
		"events.data":             "jsonb|YES",
		"events.id":               "bigint|NO",
		"events.kind":             "text|NO",
		"state.body":              "jsonb|NO",
		"state.entity":            "text|NO",
		"state.id":                "text|NO",
		"state.updated_at":        "timestamp with time zone|NO",
	}
	if len(gotCols) != len(expectedCols) {
		return fmt.Errorf("column count mismatch: got %d, want %d", len(gotCols), len(expectedCols))
	}
	for k, want := range expectedCols {
		got, ok := gotCols[k]
		if !ok || got != want {
			return fmt.Errorf("column %s mismatch: got %q, want %q", k, got, want)
		}
	}

	// Verify index definitions, including both primary keys and sort order.
	indexRows, err := tx.Query(ctx, `SELECT indexname, indexdef FROM pg_indexes WHERE schemaname='milkgraph'`)
	if err != nil {
		return err
	}
	indexes := map[string]string{}
	for indexRows.Next() {
		var name, def string
		if err := indexRows.Scan(&name, &def); err != nil {
			indexRows.Close()
			return err
		}
		indexes[name] = def
	}
	if err := indexRows.Err(); err != nil {
		indexRows.Close()
		return err
	}
	indexRows.Close()
	wantIndexes := map[string]string{
		"state_pkey":           "USING btree (entity, id)",
		"audit_pkey":           "USING btree (id)",
		"events_pkey":          "USING btree (id)",
		"audit_seeds_pkey":     "USING btree (fingerprint)",
		"state_entity_updated": "USING btree (entity, updated_at DESC, id)",
		"audit_kind_id":        "USING btree (kind, id DESC)",
	}
	if len(indexes) != len(wantIndexes) {
		return fmt.Errorf("legacy indexes mismatch: got %v", indexes)
	}
	for name, want := range wantIndexes {
		if !strings.Contains(indexes[name], want) {
			return fmt.Errorf("index %s mismatch: got %q, want %q", name, indexes[name], want)
		}
	}
	// Column defaults and primary-key constraints are part of the baseline too.
	defaultRows, err := tx.Query(ctx, `SELECT table_name, column_name, column_default FROM information_schema.columns WHERE table_schema='milkgraph' AND column_default IS NOT NULL`)
	if err != nil {
		return err
	}
	defaults := map[string]string{}
	for defaultRows.Next() {
		var table, column, value string
		if err := defaultRows.Scan(&table, &column, &value); err != nil {
			defaultRows.Close()
			return err
		}
		defaults[table+"."+column] = value
	}
	if err := defaultRows.Err(); err != nil {
		defaultRows.Close()
		return err
	}
	defaultRows.Close()
	wantDefaults := map[string]string{
		"state.updated_at": "now()", "audit.id": "nextval('milkgraph.audit_id_seq'::regclass)",
		"audit.detail": "''::text", "audit.note": "''::text", "audit.tone": "'neutral'::text",
		"audit.created_at": "now()", "events.id": "nextval('milkgraph.events_id_seq'::regclass)",
		"events.at": "clock_timestamp()",
	}
	if len(defaults) != len(wantDefaults) {
		return fmt.Errorf("legacy defaults mismatch: got %v", defaults)
	}
	for name, want := range wantDefaults {
		if defaults[name] != want {
			return fmt.Errorf("default %s: got %q, want %q", name, defaults[name], want)
		}
	}
	var constraints int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM pg_constraint c JOIN pg_namespace n ON n.oid=c.connamespace WHERE n.nspname='milkgraph' AND c.contype='p'`).Scan(&constraints); err != nil {
		return err
	}
	if constraints != 4 {
		return fmt.Errorf("expected four legacy primary keys, got %d", constraints)
	}

	// Trigger flags: BEFORE(2), DELETE(8), UPDATE(16), TRUNCATE(32); no row flag.
	var triggerFlags int
	err = tx.QueryRow(ctx, `
		SELECT t.tgtype FROM pg_trigger t
		JOIN pg_class c ON c.oid = t.tgrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'milkgraph' AND c.relname = 'audit'
		  AND t.tgname = 'trg_prevent_audit_statement' AND NOT t.tgisinternal
	`).Scan(&triggerFlags)
	if err != nil {
		return fmt.Errorf("audit trigger query: %w", err)
	}
	if triggerFlags != 58 {
		return fmt.Errorf("audit statement trigger flags mismatch: %d", triggerFlags)
	}

	// Reject a replaced or disabled audit guard, not merely a same-named trigger.
	var triggerEnabled byte
	var functionBody string
	err = tx.QueryRow(ctx, `SELECT t.tgenabled, p.prosrc FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_proc p ON p.oid=t.tgfoid WHERE n.nspname='milkgraph' AND c.relname='audit' AND t.tgname='trg_prevent_audit_statement'`).Scan(&triggerEnabled, &functionBody)
	if err != nil {
		return err
	}
	if triggerEnabled != 'O' || !strings.Contains(functionBody, "RAISE EXCEPTION 'audit log is append-only and cannot be modified or deleted'") {
		return errors.New("audit append-only guard changed or disabled")
	}
	var seqs int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM pg_sequences WHERE schemaname='milkgraph' AND sequencename IN ('hold_number','audit_id_seq','events_id_seq')`).Scan(&seqs); err != nil {
		return err
	}
	if seqs != 3 {
		return fmt.Errorf("legacy sequences mismatch: got %d of 3", seqs)
	}
	var extraTriggers int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='milkgraph' AND NOT t.tgisinternal AND t.tgname <> 'trg_prevent_audit_statement'`).Scan(&extraTriggers); err != nil {
		return err
	}
	if extraTriggers != 0 {
		return fmt.Errorf("unexpected legacy triggers: %d", extraTriggers)
	}

	return nil
}

func migrate(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS) error {
	migrations, err := readMigrations(fsys)
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}

	return pgx.BeginTxFunc(ctx, pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", migrationLock); err != nil {
			return fmt.Errorf("acquire migration lock: %w", err)
		}

		var hasVersionTable bool
		err := tx.QueryRow(ctx, "SELECT to_regclass('milkgraph.schema_migrations') IS NOT NULL").Scan(&hasVersionTable)
		if err != nil {
			return err
		}

		if !hasVersionTable {
			var hasSchema bool
			if err := tx.QueryRow(ctx, "SELECT to_regnamespace('milkgraph') IS NOT NULL").Scan(&hasSchema); err != nil {
				return err
			}

			if hasSchema {
				// Existing database without migration tracking. Verify baseline.
				if err := verifyLegacyBaseline(ctx, tx); err != nil {
					return fmt.Errorf("schema baseline validation failed: %w", err)
				}

				if _, err := tx.Exec(ctx, `
					CREATE TABLE milkgraph.schema_migrations (
						version integer PRIMARY KEY,
						checksum text NOT NULL,
						applied_at timestamptz NOT NULL DEFAULT clock_timestamp()
					);
				`); err != nil {
					return fmt.Errorf("create schema_migrations table: %w", err)
				}

				// Baseline verified: stamp version 1 with migration 1's checksum.
				if _, err := tx.Exec(ctx, `
					INSERT INTO milkgraph.schema_migrations (version, checksum)
					VALUES ($1, $2)
				`, migrations[0].version, migrations[0].checksum); err != nil {
					return fmt.Errorf("stamp baseline migration: %w", err)
				}
			} else {
				// Fresh database.
				if _, err := tx.Exec(ctx, `
					CREATE SCHEMA IF NOT EXISTS milkgraph;
					CREATE TABLE IF NOT EXISTS milkgraph.schema_migrations (
						version integer PRIMARY KEY,
						checksum text NOT NULL,
						applied_at timestamptz NOT NULL DEFAULT clock_timestamp()
					);
				`); err != nil {
					return fmt.Errorf("initialize migrations table: %w", err)
				}
			}
		}

		// Read applied versions
		rows, err := tx.Query(ctx, "SELECT version, checksum FROM milkgraph.schema_migrations ORDER BY version")
		if err != nil {
			return fmt.Errorf("query applied migrations: %w", err)
		}
		defer rows.Close()

		applied := make(map[int]string)
		var maxApplied int
		for rows.Next() {
			var v int
			var cs string
			if err := rows.Scan(&v, &cs); err != nil {
				return err
			}
			applied[v] = cs
			if v > maxApplied {
				maxApplied = v
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rows.Close()
		if len(applied) != maxApplied {
			return fmt.Errorf("migration history has a missing version: %d rows through version %d", len(applied), maxApplied)
		}

		if maxApplied > migrations[len(migrations)-1].version {
			return fmt.Errorf("database has migration version %d but embedded migrations only reach %d", maxApplied, migrations[len(migrations)-1].version)
		}

		for _, m := range migrations {
			if recordedChecksum, ok := applied[m.version]; ok {
				if recordedChecksum != m.checksum {
					return fmt.Errorf("migration %04d (%s) checksum mismatch: database has %s, embedded has %s", m.version, m.name, recordedChecksum, m.checksum)
				}
				continue
			}

			// Apply pending migration
			if _, err := tx.Exec(ctx, m.sql); err != nil {
				return fmt.Errorf("apply migration %04d (%s): %w", m.version, m.name, err)
			}

			if _, err := tx.Exec(ctx, `
				INSERT INTO milkgraph.schema_migrations (version, checksum)
				VALUES ($1, $2)
			`, m.version, m.checksum); err != nil {
				return fmt.Errorf("record migration %04d (%s): %w", m.version, m.name, err)
			}
		}

		return nil
	})
}
