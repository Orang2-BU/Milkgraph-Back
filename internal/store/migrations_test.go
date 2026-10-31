package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var migrationTestID atomic.Int64

func TestMigrationFiles(t *testing.T) {
	files := fstest.MapFS{"0001_initial.sql": &fstest.MapFile{Data: []byte("SELECT 1")}}
	ms, err := readMigrations(files)
	if err != nil || len(ms) != 1 || ms[0].version != 1 {
		t.Fatalf("load: %+v %v", ms, err)
	}
	want := sha256.Sum256([]byte("SELECT 1"))
	if ms[0].checksum != hex.EncodeToString(want[:]) {
		t.Fatalf("checksum: %+v", ms[0])
	}
	for name, files := range map[string]fs.FS{
		"gap":       fstest.MapFS{"0002_x.sql": &fstest.MapFile{Data: []byte("SELECT 1")}},
		"duplicate": fstest.MapFS{"0001_x.sql": &fstest.MapFile{Data: []byte("SELECT 1")}, "0001_y.sql": &fstest.MapFile{Data: []byte("SELECT 2")}},
		"empty":     fstest.MapFS{},
		"malformed": fstest.MapFS{"surprise.sql": &fstest.MapFile{Data: []byte("SELECT 1")}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := readMigrations(files); err == nil {
				t.Fatal("accepted invalid migrations")
			}
		})
	}
}

func isolatedMigrationDB(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to isolated PostgreSQL; migration DB tests skipped")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		t.Fatalf("TEST_DATABASE_URL must be a PostgreSQL URL: %v", err)
	}
	admin, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close(context.Background()) })
	name := fmt.Sprintf("mg_migration_%d_%d", time.Now().UnixNano(), migrationTestID.Add(1))
	// Only the test harness creates and drops this dedicated database.
	// Fixed prefix and integer: no user-controlled SQL identifier.
	if _, err := admin.Exec(context.Background(), "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP DATABASE "+name+" WITH (FORCE)"); err != nil {
			t.Errorf("drop isolated database: %v", err)
		}
	})
	u.Path = "/" + name
	return u.String()
}

func migrationConn(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	p, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

func countMigrationVersions(t *testing.T, p *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := p.QueryRow(context.Background(), "SELECT count(*) FROM milkgraph.schema_migrations").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestMigrationFreshAndRestart(t *testing.T) {
	dsn := isolatedMigrationDB(t)
	ctx := context.Background()
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	p := db.Pool
	if countMigrationVersions(t, p) != 1 {
		t.Fatal("fresh migration missing")
	}
	_, err = p.Exec(ctx, `INSERT INTO milkgraph.state(entity,id,body) VALUES ('test','kept','{"x":1}')`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if countMigrationVersions(t, db.Pool) != 1 {
		t.Fatal("restart duplicated migration")
	}
	var body string
	if err := db.Pool.QueryRow(ctx, "SELECT body::text FROM milkgraph.state WHERE id='kept'").Scan(&body); err != nil || body != `{"x": 1}` {
		t.Fatalf("state lost: %q %v", body, err)
	}
	if _, err := db.Pool.Exec(ctx, "DELETE FROM milkgraph.audit WHERE false"); err == nil {
		t.Fatal("audit trigger lost")
	}
}

func TestMigrationLegacyBaseline(t *testing.T) {
	dsn := isolatedMigrationDB(t)
	ctx := context.Background()
	p := migrationConn(t, dsn)
	// Historical db.go DDL, before the version table existed.
	_, err := p.Exec(ctx, legacySchemaSQL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Exec(ctx, `INSERT INTO milkgraph.state(entity,id,body) VALUES ('test','legacy','{"kept":true}')`)
	if err != nil {
		t.Fatal(err)
	}
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if countMigrationVersions(t, p) != 1 {
		t.Fatal("legacy version not stamped")
	}
	var n int
	if err := p.QueryRow(ctx, "SELECT count(*) FROM milkgraph.state WHERE id='legacy'").Scan(&n); err != nil || n != 1 {
		t.Fatalf("legacy data changed: %d %v", n, err)
	}
}

func TestMigrationRejectsPartialAndAlteredLegacy(t *testing.T) {
	for _, sql := range []string{
		"CREATE SCHEMA milkgraph; CREATE TABLE milkgraph.state(id text)",
		strings.Replace(legacySchemaSQL, "body jsonb NOT NULL", "body text NOT NULL", 1),
		strings.Replace(legacySchemaSQL, "CREATE INDEX IF NOT EXISTS state_entity_updated ON milkgraph.state(entity, updated_at DESC, id);", "", 1),
		strings.Replace(legacySchemaSQL, "BEFORE UPDATE OR DELETE OR TRUNCATE", "BEFORE UPDATE OR DELETE", 1),
		strings.Replace(legacySchemaSQL, "DEFAULT now()", "DEFAULT clock_timestamp()", 1),
		strings.Replace(legacySchemaSQL, "PRIMARY KEY (entity, id)", "PRIMARY KEY (id, entity)", 1),
		strings.Replace(legacySchemaSQL, "RAISE EXCEPTION 'audit log is append-only and cannot be modified or deleted';", "RETURN NULL;", 1),
		legacySchemaSQL + "CREATE TABLE milkgraph.unexpected(id integer);",
		"CREATE SCHEMA milkgraph",
	} {
		t.Run(fmt.Sprintf("case_%d", len(sql)), func(t *testing.T) {
			dsn := isolatedMigrationDB(t)
			p := migrationConn(t, dsn)
			if _, err := p.Exec(context.Background(), sql); err != nil {
				t.Fatal(err)
			}
			if db, err := Open(context.Background(), dsn); err == nil {
				db.Close()
				t.Fatal("accepted unsafe legacy schema")
			}
			var exists bool
			if err := p.QueryRow(context.Background(), "SELECT to_regclass('milkgraph.schema_migrations') IS NOT NULL").Scan(&exists); err != nil {
				t.Fatal(err)
			}
			if exists {
				t.Fatal("stamped rejected legacy schema")
			}
		})
	}
}

func TestMigrationRejectsChecksumDriftAndUnknownVersion(t *testing.T) {
	for _, version := range []int{1, 2} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			dsn := isolatedMigrationDB(t)
			db, err := Open(context.Background(), dsn)
			if err != nil {
				t.Fatal(err)
			}
			if version == 1 {
				_, err = db.Pool.Exec(context.Background(), "UPDATE milkgraph.schema_migrations SET checksum='bad' WHERE version=1")
			} else {
				_, err = db.Pool.Exec(context.Background(), "INSERT INTO milkgraph.schema_migrations(version, checksum) VALUES(2, 'unknown')")
			}
			db.Close()
			if err != nil {
				t.Fatal(err)
			}
			if db, err := Open(context.Background(), dsn); err == nil {
				db.Close()
				t.Fatal("accepted migration drift")
			}
		})
	}
}

func TestMigrationConcurrentOpen(t *testing.T) {
	dsn := isolatedMigrationDB(t)
	const workers = 8
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			db, err := Open(context.Background(), dsn)
			if err == nil {
				db.Close()
			}
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := countMigrationVersions(t, migrationConn(t, dsn)); n != 1 {
		t.Fatalf("versions = %d", n)
	}
}

func TestMigrationFailureRollsBack(t *testing.T) {
	dsn := isolatedMigrationDB(t)
	p := migrationConn(t, dsn)
	bad := fstest.MapFS{"0001_initial.sql": &fstest.MapFile{Data: []byte("CREATE SCHEMA milkgraph; CREATE TABLE milkgraph.state(id text); SELECT 1/0;")}}
	if err := migrate(context.Background(), p, bad); err == nil {
		t.Fatal("bad migration succeeded")
	}
	var exists bool
	if err := p.QueryRow(context.Background(), "SELECT to_regnamespace('milkgraph') IS NOT NULL").Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("partial migration committed")
	}
}

// Exact previous db.go:91-154 DDL, intentionally not loaded from the new migration.
const legacySchemaSQL = `
CREATE SCHEMA IF NOT EXISTS milkgraph;
CREATE TABLE IF NOT EXISTS milkgraph.state (entity text NOT NULL, id text NOT NULL, body jsonb NOT NULL, updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY (entity, id));
CREATE INDEX IF NOT EXISTS state_entity_updated ON milkgraph.state(entity, updated_at DESC, id);
CREATE TABLE IF NOT EXISTS milkgraph.audit (id bigserial PRIMARY KEY, kind text NOT NULL, title text NOT NULL, detail text NOT NULL DEFAULT '', note text NOT NULL DEFAULT '', by_user text NOT NULL, role text NOT NULL, at text NOT NULL, tone text NOT NULL DEFAULT 'neutral', body jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now());
CREATE INDEX IF NOT EXISTS audit_kind_id ON milkgraph.audit(kind, id DESC);
CREATE TABLE IF NOT EXISTS milkgraph.events (id bigserial PRIMARY KEY, kind text NOT NULL, at timestamptz NOT NULL DEFAULT clock_timestamp(), data jsonb);
CREATE SEQUENCE IF NOT EXISTS milkgraph.hold_number;
CREATE TABLE IF NOT EXISTS milkgraph.audit_seeds (fingerprint text PRIMARY KEY);
CREATE OR REPLACE FUNCTION milkgraph.prevent_audit_modification() RETURNS TRIGGER AS $$ BEGIN RAISE EXCEPTION 'audit log is append-only and cannot be modified or deleted'; END; $$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS trg_prevent_audit_modification ON milkgraph.audit;
DROP TRIGGER IF EXISTS trg_prevent_audit_statement ON milkgraph.audit;
UPDATE milkgraph.audit SET body = body || jsonb_build_object('id', id) WHERE body->'id' IS DISTINCT FROM to_jsonb(id);
CREATE TRIGGER trg_prevent_audit_statement BEFORE UPDATE OR DELETE OR TRUNCATE ON milkgraph.audit FOR EACH STATEMENT EXECUTE FUNCTION milkgraph.prevent_audit_modification();
`
