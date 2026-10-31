CREATE SCHEMA IF NOT EXISTS milkgraph;

CREATE TABLE milkgraph.state (
 entity text NOT NULL,
 id text NOT NULL,
 body jsonb NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (entity, id)
);
CREATE INDEX state_entity_updated ON milkgraph.state(entity, updated_at DESC, id);

CREATE TABLE milkgraph.audit (
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
CREATE INDEX audit_kind_id ON milkgraph.audit(kind, id DESC);

CREATE TABLE milkgraph.events (
 id bigserial PRIMARY KEY,
 kind text NOT NULL,
 at timestamptz NOT NULL DEFAULT clock_timestamp(),
 data jsonb
);

CREATE SEQUENCE milkgraph.hold_number;
CREATE TABLE milkgraph.audit_seeds (
 fingerprint text PRIMARY KEY
);

CREATE FUNCTION milkgraph.prevent_audit_modification()
RETURNS TRIGGER AS $$
BEGIN
 RAISE EXCEPTION 'audit log is append-only and cannot be modified or deleted';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_prevent_audit_statement
BEFORE UPDATE OR DELETE OR TRUNCATE ON milkgraph.audit
FOR EACH STATEMENT EXECUTE FUNCTION milkgraph.prevent_audit_modification();
