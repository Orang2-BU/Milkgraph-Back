# MilkGraph Feature Contract Coverage Matrix & Graph Architecture

> Audit date: 30 October 2026  
> Author: MilkGraph Subagent (`internal/graph`, `docs/FEATURE-COVERAGE.md`)  
> Target: `Milkgraph-Back` (Go 1.24 REST Backend) & `Milkgraph-Front` (Next.js 16 App Router)

---

## 1. Executive Summary

This document establishes the authoritative contract coverage matrix between `Milkgraph-Front` (14 operational and data pages) and `Milkgraph-Back` (Go REST service). It specifies:
1. Complete inventory of all 14 frontend pages and their REST interactions.
2. Complete mapping of documented endpoints, permissions, side effects, simulation vs actual status, and exact implementation gaps.
3. Rigorous topology and semantic audit of the Context Graph, identifying discrepancies between business labels and graph node IDs.
4. Fast-path, zero-overkill integration recommendations for the parent agent to wire `internal/graph` into `server.go`.

---

## 2. Frontend Page Inventory & REST Mapping

| # | Route | Screen / Component | Documented Endpoints Used | Mutation & Actions | State Changes & Side Effects |
|---|---|---|---|---|---|
| 1 | `/` | Receiving Flow (`receiving.tsx`) | `GET /receiving`, `GET /plans/current/check`, `GET /decisions`, `GET /limits` | None directly (navigates to `/rencana`) | Re-fetches on `snapshot.updated`, `plan.decided`, `limits.updated`, `lab.decided` |
| 2 | `/simulasi` | Simulasi 3D (`simulasi.tsx`, `plant-scene.tsx`) | `GET /site/layout`, `GET /site/status?view=`, `GET /site/bottlenecks?view=`, `GET /production/aseptic-tanks`, `GET /production/fillers`, `GET /assets/mid-tanks`, `GET /meta` | View toggle (`before` vs `after`) | Re-fetches on `snapshot.updated`, `plan.decided` |
| 3 | `/tangki` | Bay & Tangki (`tangki.tsx`) | `GET /assets`, `GET /assets/cip-log`, `GET /assets/cip-policy`, `GET /assets/mid-tanks`, `GET /plans/current`, `GET /decisions`, `GET /plans/{id}/instructions`, `GET /meta` | `POST /instructions/{id}/ack` | Acknowledges operator instruction; emits `instruction.acked` and audit log |
| 4 | `/filling` | Lini Filling (`filling.tsx`) | `GET /production/aseptic-tanks`, `GET /production/fillers`, `GET /production/unit-states`, `GET /production/changeover-matrix`, `GET /plans/current`, `GET /decisions`, `GET /plans/{id}/instructions`, `GET /meta` | `POST /instructions/{id}/ack` | Acknowledges supervisor instruction; emits `instruction.acked` and audit log |
| 5 | `/rencana` | Rencana & What-if (`rencana.tsx`) | `GET /plans`, `GET /plans/{id}`, `POST /plans/{id}/check`, `POST /plans/{id}/solve`, `GET /decisions`, `POST /plans/{id}/decision`, `GET /limits` | `POST /plans/{id}/decision`, `POST /plans/{id}/solve` | Approving plan publishes 6 instructions, updates active site status, emits `plan.decided` and audit log |
| 6 | `/pasokan` | Pasokan & Permintaan (`pasokan.tsx`) | `GET /supply/farms`, `GET /supply/demand-plan`, `GET /supply/trend`, `GET /supply/demand-sends?date=`, `POST /supply/demand/send` | `POST /supply/demand/send` | Dispatches demand to dairy farms; emits `demand.sent` and audit log |
| 7 | `/constraint` | Constraint (`constraint.tsx`) | `GET /limits`, `GET /limits/schema`, `POST /plans/current/check`, `PUT /limits` | `PUT /limits` | Mutates solver thresholds; emits `limits.updated` and audit log |
| 8 | `/persetujuan` | Riwayat Persetujuan (`persetujuan.tsx`) | `GET /audit?kind=`, `GET /audit.csv`, `GET /events` (SSE) | Realtime SSE event listener | Updates log table instantly on all decision events |
| 9 | `/trace` | Traceability (`trace.tsx`, `evidence-graph.tsx`, `genealogy.tsx`) | `GET /quality/active-event`, `GET /assistant/templates`, `POST /assistant/ask`, `GET /trace/backward`, `GET /trace/forward`, `GET /trace/explain`, `GET /trace/handoffs`, `GET /lots/locations`, `GET /holds/summary`, `POST /holds` | `POST /assistant/ask`, `POST /holds` | Queries graph paths; submits lot quarantine; emits `hold.created` and audit log |
| 10 | `/lab` | Hasil Lab (`lab.tsx`) | `GET /lab/results?kind=`, `GET /lab/summary`, `POST /lab/results/{id}/decision` | `POST /lab/results/{id}/decision` | Approving/rejecting lab release moves tanker to `Released` or removes from queue; emits `lab.decided` |
| 11 | `/hold` | Daftar Hold (`hold.tsx`) | `GET /holds`, `POST /holds`, `POST /holds/{id}/review`, `GET /holds/{id}/genealogy` | `POST /holds/{id}/review` | QA reviews hold (`aktif`, `ditolak`, `dilepas`); emits `hold.reviewed` and audit log |
| 12 | `/graph` | Context Graph (`context-graph.tsx`) | `GET /graph`, `GET /sources/{id}` | Client-side filter/selection | Visualizes complete topological bipartite graph and inspection modal |
| 13 | `/sumber-data` | Sumber Data (`sumber-data.tsx`) | `GET /data-sources`, `POST /data-sources/{id}/test`, `POST /data-sources/{id}/sync` | `POST /data-sources/{id}/test`, `POST /data-sources/{id}/sync` | Tests connectivity and triggers sync timestamp; emits `source.synced` |
| 14 | `/pengguna` | Pengguna & Peran (`pengguna.tsx`) | `GET /roles`, `GET /session`, `POST /session/role` | `POST /session/role` | Switches simulated active user role in demo mode |

---

## 3. Comprehensive REST Endpoint Coverage Matrix

| Method | Endpoint | Auth / Permission | Status | Backend Implementation | Side Effects & SSE Events | Exact Gaps / Gaps Identified |
|---|---|---|---|---|---|---|
| `GET` | `/api/v1/meta` | Public | **Actual** | `handleMeta` (`server.go`) | None | None |
| `GET` | `/healthz` | Public | **Actual** | `server.go` | None | None |
| `GET` | `/readyz` | Public | **Actual** | `server.go` (DB ping) | None | None |
| `GET` | `/api/v1/session` | Authenticated | **Actual** | `server.go` | None | None |
| `POST` | `/api/v1/session/role` | Demo mode | **Actual** | `dynamic.go` | Updates active demo role | In live mode, must reject with 400 |
| `GET` | `/api/v1/roles` | Public | **Actual** | `server.go` (seeds.Roles) | None | None |
| `GET` | `/api/v1/shifts` | Authenticated | **Actual** | `server.go` | None | None |
| `GET` | `/api/v1/notifications` | Authenticated | **Actual** | `dynamic.go` | Evaluates dynamic plan check & limits | None |
| `POST` | `/api/v1/demo/reset` | Demo mode | **Actual** | `dynamic.go` | Resets in-memory state & resets DB | None |
| `GET` | `/api/v1/receiving` | Authenticated | **Actual** | `dynamic.go` | Reflects lab decisions on tankers | None |
| `GET` | `/api/v1/assets` | Authenticated | **Actual** | `server.go` (seeds.Scenario) | None | None |
| `GET` | `/api/v1/assets/cip-log` | Authenticated | **Actual** | `server.go` (seeds.Ops) | None | None |
| `GET` | `/api/v1/assets/cip-policy` | Authenticated | **Actual** | `server.go` | None | None |
| `GET` | `/api/v1/assets/mid-tanks` | Authenticated | **Actual** | `server.go` (seeds.Ops) | None | None |
| `GET` | `/api/v1/production/aseptic-tanks` | Authenticated | **Actual** | `server.go` (seeds.Ops) | None | None |
| `GET` | `/api/v1/production/fillers` | Authenticated | **Actual** | `server.go` (seeds.Ops) | None | None |
| `GET` | `/api/v1/production/unit-states` | Authenticated | **Actual** | `server.go` (seeds.Ops) | None | None |
| `GET` | `/api/v1/production/changeover-matrix`| Authenticated | **Actual** | `server.go` (seeds.Ops) | None | None |
| `GET` | `/api/v1/plans` | Authenticated | **Actual** | `server.go` | None | None |
| `GET` | `/api/v1/plans/{id}` | Authenticated | **Actual** | `server.go` | None | None |
| `POST` | `/api/v1/plans/{id}/check` | Authenticated | **Actual** | `dynamic.go` (`rules.EvaluateResult`) | None | None |
| `POST` | `/api/v1/plans/{id}/solve` | `plan.decide` | **Simulated** | `dynamic.go` (serves planView) | Emits log; solver is simulated | Live OR-Tools MIP solver not embedded; returns deterministic planView |
| `GET` | `/api/v1/decisions` | Authenticated | **Actual** | `dynamic.go` | None | None |
| `POST` | `/api/v1/plans/{id}/decision` | `plan.decide` | **Actual** | `dynamic.go` | Creates instructions, audits log, emits `plan.decided` | None |
| `GET` | `/api/v1/limits` | Authenticated | **Actual** | `dynamic.go` | None | None |
| `GET` | `/api/v1/limits/schema` | Authenticated | **Actual** | `limits.go` | None | None |
| `PUT` | `/api/v1/limits` | `constraint.edit` | **Actual** | `dynamic.go` | Mutates limits, audits diff, emits `limits.updated` | None |
| `GET` | `/api/v1/plans/{id}/instructions` | Authenticated | **Actual** | `dynamic.go` | Reflects ack status | Returns empty slice until plan `current` approved |
| `POST` | `/api/v1/instructions/{id}/ack` | `instruction.ack`| **Actual** | `dynamic.go` | Checks role match, audits, emits `instruction.acked` | Rejects duplicate ack with 409 |
| `GET` | `/api/v1/supply/farms` | Authenticated | **Actual** | `server.go` (seeds.Ops) | None | None |
| `GET` | `/api/v1/supply/demand-plan` | Authenticated | **Actual** | `server.go` (seeds.Ops) | None | None |
| `GET` | `/api/v1/supply/trend` | Authenticated | **Actual** | `server.go` (seeds.Ops) | None | None |
| `GET` | `/api/v1/supply/demand-sends` | Authenticated | **Actual** | `dynamic.go` | None | None |
| `POST` | `/api/v1/supply/demand/send` | `demand.send` | **Actual** | `dynamic.go` | Enforces once-per-day, emits `demand.sent` | None |
| `GET` | `/api/v1/quality/active-event` | Authenticated | **Actual** | `server.go` (seeds.Scenario) | None | None |
| `GET` | `/api/v1/assistant/templates` | Authenticated | **Actual** | `trace/service.go` | None | None |
| `POST` | `/api/v1/assistant/ask` | Authenticated | **Actual** | `trace/ask.go` | Dispatches to template queries, abstains on unknown | Discrepancy: Question targets are business codes (`FG-1013-07`) |
| `GET` | `/api/v1/trace/backward` | Authenticated | **Actual** | `trace/service.go` | Returns backward answer | Lot ID parameter is `FG-1013-07` (business code) |
| `GET` | `/api/v1/trace/forward` | Authenticated | **Actual** | `trace/service.go` | Returns forward answer | RT ID parameter is `RT-04` (business code) |
| `GET` | `/api/v1/trace/explain` | Authenticated | **Actual** | `trace/service.go` | Returns bay2 answer | Resource parameter is `Bay 2` (business label) |
| `GET` | `/api/v1/trace/handoffs` | Authenticated | **Actual** | `trace/service.go` | Returns handoff records | Endpoints in handoffs use business labels & composite strings |
| `GET` | `/api/v1/lots/locations` | Authenticated | **Actual** | `server.go` (seeds.Ops) | None | Max 100 IDs validated |
| `GET` | `/api/v1/lab/results` | Authenticated | **Actual** | `dynamic.go` | Attaches decisions & recalculates ETAs | None |
| `GET` | `/api/v1/lab/summary` | Authenticated | **Actual** | `dynamic.go` | None | None |
| `POST` | `/api/v1/lab/results/{id}/decision` | `lab.decide` | **Actual** | `dynamic.go` | Updates tanker stage, audits, emits `lab.decided` | None |
| `GET` | `/api/v1/holds` | Authenticated | **Actual** | `dynamic.go` | Reverse chronological order | None |
| `GET` | `/api/v1/holds/summary` | Authenticated | **Actual** | `server.go` (seeds.Scenario) | None | None |
| `POST` | `/api/v1/holds` | `hold.submit` | **Actual** | `dynamic.go` | Auto-assigns `HOLD-001`, emits `hold.created` | Prevents duplicate active trigger with 409 |
| `POST` | `/api/v1/holds/{id}/review` | `hold.review` | **Actual** | `dynamic.go` | State machine validation, emits `hold.reviewed`| Transitions: `diajukan` → `aktif`/`ditolak`; `aktif` → `dilepas` |
| `GET` | `/api/v1/holds/{id}/genealogy` | Authenticated | **Actual** | `dynamic.go` | Assembles hold, handoffs, lab, lots | None |
| `GET` | `/api/v1/audit` | Authenticated | **Actual** | `dynamic.go` | Filters by kind, reverse chronological | None |
| `GET` | `/api/v1/audit.csv` | Authenticated | **Actual** | `dynamic.go` | Streams UTF-8 BOM CSV | None |
| `GET` | `/api/v1/events` | Authenticated | **Actual** | `sse/hub.go` | Broadcasts realtime SSE events | None |
| `GET` | `/api/v1/graph` | Authenticated | **Actual** | `server.go` (seeds.Scenario["graph"]) | None | Currently serves raw static fixture; should use `internal/graph.Service` |
| `GET` | `/api/v1/data-sources` | Authenticated | **Actual** | `dynamic.go` | None | Feeds correspond to graph nodes |
| `POST` | `/api/v1/data-sources/{id}/test` | Authenticated | **Actual** | `dynamic.go` | Simulates latency (12-45ms) | Read-only simulated ping |
| `POST` | `/api/v1/data-sources/{id}/sync` | Authenticated | **Actual** | `dynamic.go` | Updates `lastSync`, emits `source.synced` | Live external sync connector simulated |
| `GET` | `/api/v1/site/layout` | Authenticated | **Actual** | `server.go` (seeds.Site) | None | None |
| `GET` | `/api/v1/site/status` | Authenticated | **Actual** | `dynamic.go` | Reflects `before` vs `after` view | None |
| `GET` | `/api/v1/site/bottlenecks` | Authenticated | **Actual** | `dynamic.go` | Prioritizes bottlenecks by focus | None |

---

## 4. Graph Topology & Semantic Discrepancy Audit

### 4.1 16 Valid Node Types Contract
`Milkgraph-Front` (`components/trace/evidence-graph.tsx` glyphs and `lib/graph.ts` KIND_OF) strictly categorizes 16 node types into 4 visual kinds:
- **asal (2 types):** `Farm`, `RT`
- **mutu (2 types):** `LabResult`, `CIPEvent`
- **proses (11 types):** `Bay`, `Buffer`, `Thermal`, `Separator`, `Storage`, `Cream`, `Premix`, `Mixing`, `Sterilisasi`, `Aseptic`, `Filling`
- **lot (1 type):** `Lot`

`internal/graph.Service` validates that every node strictly belongs to these 16 types.

### 4.2 Exact Audit of Discrepancies: Semantic Business Labels vs. Graph Node IDs

A critical structural finding was audited between the raw fixture data and query interfaces:

| Domain Item | Business Label / Semantic Reference | Canonical Graph Node ID | Component Affected | Root Cause & Resolution |
|---|---|---|---|---|
| Trace Backward Query | `FG-1013-07` | `fg-07` | `internal/trace/ask.go`, `trace.go` | `Ask` regex extracts `FG-1013-07`. Target node in graph is `fg-07`. The `Backward` method matches `FG-1013-07`. Safe fix: Maintain label lookup table so queries by either `fg-07` or `FG-1013-07` resolve cleanly. |
| Trace Forward Query | `RT-04` | `rt-04` | `internal/trace/ask.go`, `trace.go` | `Forward` checks `rtID == "RT-04"`. Graph node id is `rt-04`. |
| Trace Explain Query | `Bay 2` | `bay-2` | `internal/trace/ask.go`, `trace.go` | `Explain` checks `resource == "Bay 2"`. Graph node id is `bay-2`. |
| Backward `mix[].id` | `"RT-04"`, `"RT-02"`, `"RT-06"`, `"RT-09"` | `rt-04`, `rt-02`, `rt-06`, `rt-09` | `components/trace/genealogy.tsx` | Mix IDs in `scenario.answers[0].mix` are business labels (`RT-04`), while `scenario.answers[0].nodes` contains canonical graph node IDs (`rt-04`). |
| Forward `lots[].id` | `"FG-1013-05"` ... `"FG-1013-09"` | `fg-05` ... `fg-09` | `components/trace/genealogy.tsx` | Lot items in `scenario.answers[1].lots` use business codes. `scenario.answers[1].nodes` uses canonical graph node IDs (`fg-05`..`fg-09`). |
| Handoffs composite `from`/`to` | `"TH-1, SP-1"` | `th-1`, `sp-1` | `internal/trace/handoffs` | Handoff step 3 points `to: "TH-1, SP-1"`. In the graph, these are two discrete nodes `th-1` and `sp-1` connected by edge `e-th-sp`. |
| Handoffs composite `from`/`to` | `"ST-4 + PM-3"` | `st-4`, `pm-3` | `internal/trace/handoffs` | Handoff step 5 points `from: "ST-4 + PM-3"`. In the graph, `st-4` and `pm-3` feed into `mx-2`. |
| Handoffs composite `from`/`to` | `"UHT-2, AT-2"` | `uht-1`, `at-2` | `internal/trace/handoffs` | Handoff step 6 points `to: "UHT-2, AT-2"`. In the graph, `uht-1` (label `"UHT-2"`) connects to `at-2`. |
| Node Provenance `source` | `TR-RMT2`, `FARM-B`, `LAB-8812`, etc. | Not a node endpoint | `internal/graph.Service` | Node property `source` points to `scenario.sources[].id`, not another graph node. The edge validator correctly enforces that edge `source` and `target` point to nodes, while node `source` points to provenance sources. |

### 4.3 Actual Temporal & Contextual Relations Rendered in Graph
The context graph contains four operational facts represented as edges and attributes:
1. `e-rt26-bay2`: `source: "rt-26"`, `target: "bay-2"`, `label: "PLANNED 14.50"`, `planned: true`. Represents a future, unscheduled unloading slot that triggers the constraint violation.
2. `e-cip-bay2`: `source: "cip-b2"`, `target: "bay-2"`, `label: "OCCUPIES 14.10 - 15.20"`. Explains why Bay 2 is unavailable during RT-26's arrival window.
3. `e-lab-rt26`: `source: "lab-gate"`, `target: "rt-26"`, `label: "RELEASED 14.18"`. Captures the sampling and lab release timestamp.
4. `e-at2-f4`: `source: "at-2"`, `target: "f4"`, `label: "TIDAK MENGISI 13 Okt"`. Explains why Filler F4 did not receive milk on October 13, isolating the downstream contamination risk to Filler F2 only.

---

## 5. Parent Integration Recommendations for `internal/graph`

To integrate `internal/graph.Service` with minimal overhead (no Neo4j overbuild, sub-millisecond execution, pure Go):

### Step 1: Add Graph Service to `Server` struct (`internal/api/server.go`)
```go
import "milkgraph/internal/graph"

type Server struct {
    // ...
    graph *graph.Service
}
```

### Step 2: Initialize in `Handler()`
```go
s.graph, err = graph.New()
if err != nil {
    panic(err)
}
```

### Step 3: Replace static `/api/v1/graph` handler
Instead of serving `seeds.Scenario["graph"]`, return `s.graph.View()`:
```go
register("GET /api/v1/graph", "", func(r *http.Request) (any, error) {
    return s.graph.View(), nil
})
```

### Step 4: Add Subgraph endpoint for targeted FE exploration
```go
register("GET /api/v1/graph/subgraph", "", func(r *http.Request) (any, error) {
    node := r.URL.Query().Get("node")
    depthStr := r.URL.Query().Get("depth")
    depth := 1
    if depthStr != "" {
        fmt.Sscanf(depthStr, "%d", &depth)
    }
    v, err := s.graph.Subgraph(node, depth)
    if err != nil {
        if errors.Is(err, graph.ErrNotFound) {
            return nil, missing()
        }
        return nil, invalid(err.Error(), "depth")
    }
    return v, nil
})
```

### Step 5: Dynamic State Projection
When a new lot or tanker is added dynamically via API, the parent can call:
```go
projected, err := s.graph.Project(map[string]any{
    "nodes": []any{newLotNode},
    "edges": []any{newFillEdge},
})
if err == nil {
    s.dynGraph = projected // Serve live projected context graph
}
```
This preserves the baseline fixture while providing live dynamic context graph propagation for the frontend.
