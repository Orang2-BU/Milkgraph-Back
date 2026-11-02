# Milkgraph Backend (Go)

REST JSON `/api/v1` untuk frontend `Milkgraph-Front`. [Swagger 2.0 endpoint aktual](docs/swagger.yaml) (impor ke Swagger UI/Editor; file ini tidak disajikan sebagai route HTTP). Kontrak frontend: `../Milkgraph-Front/docs/04-API-CONTRACT.md`.
Validasi: `npx -y --package @apidevtools/swagger-cli swagger-cli validate docs/swagger.yaml`.

## Status jujur
- Route operasional lama tetap memakai fixture simulasi (`seed/*.json`, snapshot beku 14.42 WIB). Bukan konektor sistem nyata (PLC/SCADA/LIMS).
- Master Greenfields v2.1 dari 13 CSV asli tersedia terpisah di `seed/master/`; dimuat saat startup, divalidasi referensinya, dan hanya dibaca lewat `GET /api/v1/master`, `/master/assets`, `/master/connections`, `/master/products`, `/master/finished-skus`, `/master/lines`, `/master/rules` (header `X-Demo-Role` di mode demo atau JWT di mode live; header demo bukan autentikasi aman). Ini **belum** diimpor ke PostgreSQL dan belum mengganti graph/operasional frontend.
- Nilai `X`, `N/A`, dan kosong dipertahankan sebagai string berbeda. `numericalPlanningReady=false`; jangan substitusi nilai asumsi ataupun menyebutnya data aktual.
- `asset_connections` adalah route yang diizinkan, bukan transfer nyata; `via_asset_id` adalah sumber daya pipa, bukan bukti event. Trace/genealogi dan solver lama tetap berbasis fixture demo.
- Keputusan, hold, lab, limit, audit demo disimpan di PostgreSQL (audit append-only, event outbox untuk SSE).
- Frontend sudah punya mode API, tapi UI operasionalnya masih merender skenario demo, belum master baru.

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
