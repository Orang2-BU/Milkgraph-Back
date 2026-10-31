# Visualisasi Database MilkGraph

Dokumen ini mendeskripsikan arsitektur dan relasi penyimpanan PostgreSQL pada `Milkgraph-Back` (`schema: milkgraph`).

---

## 1. Entity-Relationship Diagram (PostgreSQL)

```mermaid
erDiagram
    STATE {
        text entity PK "Namespace entitas (limits, decision, hold, dll)"
        text id PK "Identifier unik per entitas"
        jsonb body "Payload JSONB dokumen state"
        timestamptz updated_at "Waktu pembaruan terakhir (clock_timestamp)"
    }

    AUDIT {
        bigserial id PK "Identifier unik log berurutan"
        text kind "Kategori audit (plan, instruction, hold, dll)"
        text title "Judul ringkas event"
        text detail "Deskripsi rinci"
        text note "Catatan pengguna / alasan"
        text by_user "Nama pengguna yang melakukan aksi"
        text role "Peran pengguna (Planner, QA/Lab, dll)"
        text at "Waktu operasional tampilan (WIB)"
        text tone "Indikator visual UI (neutral, positive, warning, destructive)"
        jsonb body "Payload detail log lengkap"
        timestamptz created_at "Timestamp audit dibuat (now)"
    }

    EVENTS {
        bigserial id PK "ID event outbox urut"
        text kind "Nama event domain SSE (plan.decided, hold.created, dll)"
        timestamptz at "Timestamp event outbox (clock_timestamp)"
        jsonb data "Payload data perubahan event"
    }

    AUDIT_SEEDS {
        text fingerprint PK "Hash unik bootstrap log historis"
    }

    HOLD_NUMBER_SEQ {
        bigint nextval "Sequence counter untuk ID HOLD-NNN"
    }

    STATE ||..o{ EVENTS : "memicu event SSE outbox"
    AUDIT ||..o{ EVENTS : "terekam bersamaan saat mutasi domain"
```

---

## 2. Diagram Alur Transaksi & State Machine

```mermaid
flowchart TD
    subgraph Client ["Client (Frontend / Operator)"]
        Req["REST Request (POST / PUT)"]
    end

    subgraph LockEngine ["Locking & Concurrency"]
        AdvLock["PostgreSQL Transaction Advisory Lock\npg_advisory_xact_lock(hash(entity + id))"]
    end

    subgraph Storage ["Schema milkgraph"]
        StateTbl[("milkgraph.state\n(Entity, ID, Body)")]
        AuditTbl[("milkgraph.audit\n(Append-Only Log)")]
        EventsTbl[("milkgraph.events\n(SSE Outbox Queue)")]
        Seq[("milkgraph.hold_number\n(Sequence)")]
    end

    subgraph Security ["Immutability Trigger"]
        AuditTrg{"BEFORE UPDATE / DELETE / TRUNCATE\nprevent_audit_modification()"}
        RaiseEx["RAISE EXCEPTION\n(Audit log is append-only)"]
    end

    subgraph SSEStream ["Server-Sent Events"]
        Hub["SSE Broadcast Hub\n(internal/sse)"]
        Subscribers["Connected Browsers / Tabs"]
    end

    Req --> AdvLock
    AdvLock --> StateTbl
    AdvLock --> AuditTbl
    AdvLock --> EventsTbl
    Req -.->|Permintaan Hold Baru| Seq

    AuditTbl --> AuditTrg
    AuditTrg -->|Jika ada Update/Delete| RaiseEx

    EventsTbl -->|Poll / Stream Outbox| Hub
    Hub --> Subscribers
```

---

## 3. Spesifikasi Tabel & Objek Database

| Objek | Tipe | Keterangan & Invarian |
|---|---|---|
| `milkgraph.state` | Table | Key-value store fleksibel (`entity`, `id`) bertipe JSONB untuk persistensi dokumen dinamis (limits, decisions, holds, instructions ack). Diproteksi advisory lock per entitas saat mutasi. |
| `milkgraph.audit` | Table | Ledger audit log aktivitas seluruh pengguna. Dilindungi trigger `prevent_audit_modification()` sehingga bersifat mutlak **append-only** (tidak dapat di-`UPDATE`, `DELETE`, atau `TRUNCATE`). |
| `milkgraph.events` | Table | Outbox queue untuk event SSE real-time (`plan.decided`, `instruction.acked`, `limits.updated`, `hold.created`, `source.synced`). |
| `milkgraph.audit_seeds` | Table | Pencatat fingerprint bootstrap seed histori audit agar tidak terduplikasi saat restart backend. |
| `milkgraph.hold_number` | Sequence | Penomoran urut generator kode hold (`HOLD-001`, `HOLD-002`, dst). |
| `state_entity_updated` | B-tree Index | Indeks komposit `(entity, updated_at DESC, id)` untuk query list & sinkronisasi cepat. |
| `audit_kind_id` | B-tree Index | Indeks komposit `(kind, id DESC)` untuk filtering log riwayat persetujuan. |
