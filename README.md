# Milkgraph Backend (Go)

REST JSON `/api/v1` untuk frontend `Milkgraph-Front`. Kontrak: `../Milkgraph-Front/docs/04-API-CONTRACT.md`.

## Status jujur
- Data pabrik = fixture simulasi (`seed/*.json`, snapshot beku 14.42 WIB). Bukan konektor sistem nyata (PLC/SCADA/LIMS).
- Rules engine, trace/genealogi, graph dihitung di Go dari fixture. Bukan solver CP-SAT umum.
- Keputusan, hold, lab, limit, audit disimpan di PostgreSQL (audit append-only, event outbox untuk SSE).
- Frontend belum terintegrasi; backend baru diuji lewat HTTP test.

## Jalankan
```
cp .env.example .env   # isi POSTGRES_PASSWORD
docker compose up --build
curl http://127.0.0.1:8080/api/v1/meta
```
Native: set `DATABASE_URL`, lalu `go run ./cmd/server` (bind default `127.0.0.1:8080`).

## Konfigurasi
- `MODE`: `demo` (header `X-Demo-Role`) atau `live` (JWT HS256, wajib `JWT_SECRET` >= 32 karakter, `JWT_ISSUER`, `JWT_AUDIENCE`).
- `CORS_ORIGINS`: allowlist eksplisit, tanpa `*`.
- `DATABASE_POOL_MAX`: 1-100 (default 20).

## Tes
```
go vet ./...
go test ./...
TEST_DATABASE_URL=... go test ./internal/store ./internal/sse   # butuh PostgreSQL terisolasi
```
