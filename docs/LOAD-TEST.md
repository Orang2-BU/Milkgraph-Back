# Network Load & Concurrency Test Report

## Executive Summary

Real network load and concurrency tests were executed against the Milkgraph backend running as a live TCP HTTP server (`httptest.NewServer`) backed by a dedicated, disposable PostgreSQL database instance. No mock handlers or artificial in-memory stubs were used.

- **1,000 Concurrent Virtual Users (REST)**: Completed 89,189 network requests over 35.5s (30s sustained window) with **0 unexpected errors** (`error_rate = 0.000000`). Sustained throughput reached **2,514.25 req/s** with **p50 = 132.93ms**, **p95 = 1,164.24ms** (< 5s threshold), and **max = 3,515.18ms**.
- **Contention & Mutex Verification**: Under simultaneous plan decision submissions from all 1,000 users, exactly **1** succeeded (`200 OK`) and **999** were correctly rejected with state conflict (`409 CONFLICT`).
- **1,000 Simultaneous SSE Connections**: 1,000 active SSE HTTP streams maintained simultaneously. When a mutation was applied on a completely separate, independent API instance against the shared database, **all 1,000 connections received the event** within **p95 = 202.09ms**. `Last-Event-ID` durable replay was verified, followed by clean concurrent shutdown in 92.17ms.

---

## Test Environment & Hardware

| Parameter | Value |
|---|---|
| Operating System | Windows 11 (amd64) |
| Go Version | `go1.24.4 windows/amd64` |
| Hardware CPU | AMD64 Family 23 Model 104 Stepping 1, AuthenticAMD |
| Logical Cores / GOMAXPROCS | 8 / 8 |
| Database | PostgreSQL 16 Alpine (`docker: milkgraph-test-db` on `127.0.0.1:5549`) |
| DB Pool Max Connections | 20 (`pgxpool`) |
| Client Network Transport | `MaxConnsPerHost: 2000`, `MaxIdleConns: 2000`, `MaxIdleConnsPerHost: 2000` |
| Dataset | Context Graph: 30 nodes, 28 edges; Audit initial: 4 rows |
| Isolation Strategy | Each test run provisions a unique disposable database (`milkgraph_load_<pid>_<nanos>`), verified via `SELECT current_database()`, and drops it on cleanup. |

---

## Test Execution Details

### 1. TestNetworkSourceSyncIsDurable
- **Purpose**: Verify HTTP mutation requests commit durable PostgreSQL event and state records before returning success.
- **Duration**: 0.37s
- **Status**: `PASS`
- **Verification**: `POST /api/v1/data-sources/{id}/sync` incremented PostgreSQL event log with kind `source.synced`.

### 2. TestNetworkLoad1000Users
- **Users**: 1,000 concurrent virtual users
- **Profile**: 5s ramp-up to 1,000 users, 30s sustained measurement window, 50–200ms think time between requests.
- **Total Requests**: 89,189 (83,165 sustained)
- **Duration**: 35.47s
- **Throughput**: 2,514.25 req/s
- **Error Counts**:
  - `200 OK`: 88,190
  - `409 CONFLICT` (Expected decision race): 999
  - Unexpected 4xx / 5xx / Network Drops: **0**
  - Unexpected Error Rate: **0.000000%**
- **Latency Percentiles**:
  - `p50`: 132.93ms
  - `p95`: 1,164.24ms (1.16s)
  - `p99`: 1,865.99ms (1.87s)
  - `max`: 3,515.18ms (3.52s)
- **Request Mix Distribution**:
  - `GET /api/v1/trace/forward?rtId=RT-04&date=2026-10-13`: 7,473
  - `GET /api/v1/trace/backward?lotId=FG-1013-07`: 7,408
  - `GET /api/v1/audit?limit=100`: 7,387
  - `POST /api/v1/plans/current/check`: 7,367
  - `GET /api/v1/holds`: 7,362
  - `GET /api/v1/receiving`: 7,345
  - `GET /api/v1/plans/current`: 7,340
  - `GET /api/v1/limits`: 7,339
  - `GET /api/v1/graph`: 7,329
  - `GET /api/v1/supply/demand-plan`: 7,280
  - `GET /api/v1/lab/results`: 7,260
  - `GET /api/v1/data-sources`: 6,799
  - `POST /api/v1/plans/current/decision`: 1,000 (1 approved, 999 conflict)
  - `POST /api/v1/data-sources/scada/sync`: 500 (durable writes)
- **Database Growth**:
  - State rows: 0 → 2
  - Audit rows: 4 → 5
  - Event rows: 0 → 501 (<100k safety boundary respected)

### 3. TestNetworkSSE1000Connections
- **Concurrent SSE Streams**: 1,000 simultaneous persistent connections (`text/event-stream`).
- **Connection Establishment**: 2.00s total open duration (paced across 2ms intervals to prevent OS SYN backlog saturation). 0 drops.
- **Cross-Instance Mutation Propagation**:
  - Secondary API instance (`writer`) updated source sync state directly in PostgreSQL.
  - Primary API instance (`server`) detected the event via poller and broadcasted to all 1,000 subscribers.
  - **Delivered**: 1,000 / 1,000 clients received identical event ID, event kind, and payload bytes.
- **Delivery Latencies**:
  - `p50`: 195.81ms
  - `p95`: 202.09ms
  - `p99`: 203.66ms
  - `max`: 204.23ms
- **Durable Replay (`Last-Event-ID`)**:
  - Reconnecting client with `Last-Event-ID: 1` successfully received missed event `2`.
- **Clean Shutdown**:
  - Context cancellation terminated all 1,000 streams and HTTP listeners in 92.17ms (<5s threshold).

---

## How to Rerun

The network load suite is gated behind `RUN_NETWORK_LOAD=1` and requires `TEST_DATABASE_URL` pointing to disposable local PostgreSQL with `CREATEDB` permissions.

### Option A: Automated Extraction from Docker

```bash
python -c 'import os,json,subprocess,urllib.parse; env=os.environ.copy(); info=json.loads(subprocess.check_output(["docker","inspect","milkgraph-test-db"],text=True))[0]; kv=dict(x.split("=",1) for x in info["Config"]["Env"] if "=" in x); url=urllib.parse.urlunsplit(("postgres",urllib.parse.quote(kv.get("POSTGRES_USER","postgres"),safe="")+":"+urllib.parse.quote(kv["POSTGRES_PASSWORD"],safe="")+"@127.0.0.1:5549","/milkgraph","sslmode=disable","")); env.update(RUN_NETWORK_LOAD="1",TEST_DATABASE_URL=url); raise SystemExit(subprocess.run(["go","test","./internal/api","-run","^TestNetwork","-count=1","-v","-timeout=5m"],env=env).returncode)'
```

### Option B: Direct Shell Execution

```bash
export RUN_NETWORK_LOAD=1
export TEST_DATABASE_URL="postgres://postgres:<password>@127.0.0.1:5549/milkgraph?sslmode=disable"
go test ./internal/api -run "^TestNetwork" -count=1 -v -timeout=5m
```

### Optional Test Parameters
- `NETWORK_LOAD_DURATION`: Override sustained duration (default `30s`, allowed `20s` to `5m`). Example: `NETWORK_LOAD_DURATION=45s`.
