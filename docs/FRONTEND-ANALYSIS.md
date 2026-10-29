# MilkGraph Frontend & Documentation Audit: Comprehensive System Analysis

> **Authoritative Technical Specification & Gap Analysis**  
> **Target:** `Milkgraph-Front` (Next.js 16 App Router) and `Milkgraph-Back` (Go REST Backend Target)  
> **Date:** 29 Oktober 2026  
> **Status:** Completed Exhaustive Audit

---

## 1. Executive Summary & Programmatic Inventory Audit

### 1.1 Scope & Verification Ledger
An exhaustive, programmatic audit of the entire `Milkgraph-Front` repository and the complete `docs/` documentation suite was conducted. Every text document, specification, data fixture, configuration file, component, library module, and test script was read and verified.

#### Programmatic File Inventory (`Milkgraph-Front`)
- **Total Non-Ignored Files:** 224 files
- **Top-level Distribution:**
  - `docs/`: 88 files (documentation, brand guidelines, SVG logos, PNG renders, Python asset generators)
  - `mockup/`: 57 files (UI mockups across responsive viewports: 375px, 768px, 1440px)
  - `app/`: 20 files (14 route pages, Root Layout, NotFound handler, Health API route, Global CSS, icons)
  - `components/`: 20 files (UI screens, interactive Gantt timeline, 3D Three.js canvas, Graph visualizers, shared design system)
  - `mock/`: 10 files (TypeScript types, operational fixtures, plant layout, and 6 JSON export fixtures)
  - `lib/`: 7 files (core rules engine, demo state store, role RBAC matrix, graph layout, formatters, nav)
  - `tests/`: 4 files (Rules engine TAP test, Scenario consistency test, Docker deployment test, Playwright browser test)
  - `scripts/`: 2 files (Fixture export generator `export-fixtures.mjs`, TypeScript hook loader `ts-hooks.mjs`)
  - Root & Config: 16 files (`package.json`, `pnpm-lock.yaml`, `tsconfig.json`, `next.config.ts`, `Dockerfile`, `.github/workflows/ci.yml`, `.env.example`, `PRODUCT.md`, etc.)

#### Total Codebase Metrics
- **TypeScript / JavaScript Code Files:** 57 files
- **Total Executable/Declarative Code Lines:** 2,612 lines (excluding JSON fixtures and Markdown docs)
- **Active Test Suite:** 9/9 unit tests passing (`node --experimental-strip-types --test tests/scenario.test.mjs tests/rules.test.mjs`), clean TypeScript compile (`tsc --noEmit`), and zero lint warnings (`eslint .`).

---

### 1.2 Comprehensive Inventory of `docs/` Suite (88 Files)

| File / Directory | Ext | Lines / Count | File Size (Bytes) | Role & Audit Assessment |
|---|---|---|---|---|
| `docs/01-PRD.md` | `.md` | 338 lines | 16,607 | **Active Core**: Product requirements, 14 routes, user personas, Greenfields bootcamp mapping, problem statements. |
| `docs/02-PLAN.md` | `.md` | 134 lines | 6,253 | **Active Core**: Execution phases, simulation scenario facts, verification milestones. |
| `docs/03-ARCHITECTURE.md` | `.md` | 275 lines | 13,012 | **Active Core**: System architecture, module diagrams, Neo4j + Postgres schemas, decision records. |
| `docs/04-API-CONTRACT.md` | `.md` | 468 lines | 26,489 | **Active Core**: Complete REST `/api/v1` contract, SSE event definitions, error codes, and field models. |
| `docs/05-INTEGRASI-FE-BE.md` | `.md` | 448 lines | 25,577 | **Active Core**: Comprehensive migration plan, static-to-API inventory, BE milestone roadmap (M0-M8), test checklist. |
| `docs/06-DEPLOY-DOKPLOY.md` | `.md` | 148 lines | 7,122 | **Active Core**: Containerization, Dockerfile standalone build, Dokploy deployment guide, healthchecks. |
| `docs/08-DESIGN.md` | `.md` | 265 lines | 12,590 | **Active Core**: Visual design system, color tokens, typography (Onest + JetBrains Mono), glassmorphism specs. |
| `docs/README.md` | `.md` | 42 lines | 1,778 | **Active Core**: Index of documentation and guidelines for backend sync. |
| `docs/PROMPT-CODEX.md` | `.md` | 245 lines | 12,667 | **Archived / Obsolete**: Initial bootstrap prompts. Kept for historical reference. |
| `docs/RANGKUMAN-LENGKAP-MILKGRAPH.md`| `.md` | 1,083 lines| 57,187 | **Contextual Reference**: Detailed domain research, partner problem breakdown, early architectural rationale. |
| `docs/brand/final/GUIDELINES.md` | `.md` | 74 lines | 2,296 | **Active Core**: Logo usage, color codes (`#1B4332`, `#2D6A4F`, `#40916C`, `#52B788`, `#D8F3DC`), clearspace. |
| `docs/brand/final/board-spec.json` | `.json` | 68 lines | 1,560 | Brand design token specifications. |
| `docs/brand/final/board.html` | `.html` | 2,130 lines | 99,252 | Visual design board renderer. |
| `docs/brand/final/build.py` | `.py` | 108 lines | 3,946 | Python build script exporting brand SVGs to web-ready PNGs and ICO favicon. |
| `docs/brand/gen.py` | `.py` | 72 lines | 2,240 | Python SVG generator script for initial logo concepts. |
| `docs/brand/gen2.py` | `.py` | 48 lines | 1,496 | Python SVG generator script for secondary logo iterations. |
| `docs/brand/final/web/site.webmanifest`| `.webmanifest`| 19 lines | 448 | PWA / web application manifest. |
| `docs/brand/final/web/head-snippet.html`| `.html`| 8 lines | 261 | HTML `<head>` tags for brand icons. |
| `docs/brand/*.svg` | `.svg` | 38 files | 300 - 14,136 | Brand lockups, symbols, wordmarks, app tiles, and concept vector art. Verified valid XML/SVG. |
| `docs/brand/*.png` | `.png` | 31 files | 406 - 86,200 | Derivative raster renderings, icon sizes (16, 32, 48, 192, 512), and presentation slide graphics. |
| `docs/brand/final/web/favicon.ico` | `.ico` | Binary | 2,050 | Multi-resolution favicon icon file. |

---

## 2. Complete 14-Route Architecture & Resource Mapping

Every route in `Milkgraph-Front` is an interactive, fully styled screen wired to the shared demo state or domain data. Below is the exhaustive route-to-resource data flow mapping:

| # | Route | Route File (`app/`) | Component File (`components/`) | Primary GET Endpoints Required | Mutations / Writes Triggered | SSE Invalidation Triggers |
|---|---|---|---|---|---|---|
| 1 | `/` | `app/page.tsx` | `components/receiving/receiving.tsx` | `GET /receiving`, `GET /plans/current/check`, `GET /decisions`, `GET /limits` | None directly (links to `/rencana`) | `snapshot.updated`, `plan.decided`, `limits.updated`, `lab.decided` |
| 2 | `/simulasi` | `app/simulasi/page.tsx` | `components/simulasi/simulasi.tsx` & `plant-scene.tsx` | `GET /site/layout`, `GET /site/status?view=`, `GET /site/bottlenecks?view=`, `/production/aseptic-tanks`, `/production/fillers`, `/assets/mid-tanks`, `/meta` | Toggle `view` (`before` vs `after`) | `snapshot.updated`, `plan.decided` |
| 3 | `/tangki` | `app/tangki/page.tsx` | `components/tangki/tangki.tsx` | `GET /assets`, `GET /assets/cip-log`, `GET /assets/cip-policy`, `GET /assets/mid-tanks`, `GET /plans/current`, `GET /decisions`, `GET /plans/current/instructions`, `GET /meta` | `POST /instructions/{id}/ack` | `plan.decided`, `instruction.acked`, `snapshot.updated` |
| 4 | `/filling` | `app/filling/page.tsx` | `components/filling/filling.tsx` | `GET /production/aseptic-tanks`, `GET /production/fillers`, `GET /production/unit-states`, `GET /production/changeover-matrix`, `GET /plans/current`, `GET /decisions`, `GET /plans/current/instructions`, `GET /meta` | `POST /instructions/{id}/ack` | `plan.decided`, `instruction.acked`, `snapshot.updated` |
| 5 | `/rencana` | `app/rencana/page.tsx` | `components/rencana/rencana.tsx` | `GET /plans`, `GET /plans/{id}/check`, `GET /decisions`, `GET /limits`, `GET /session` | `POST /plans/{id}/decision`, `POST /plans/{id}/solve` | `plan.decided`, `limits.updated`, `plan.solved` |
| 6 | `/pasokan` | `app/pasokan/page.tsx` | `components/pasokan/pasokan.tsx` & `line-chart.tsx` | `GET /supply/farms`, `GET /supply/demand-plan`, `GET /supply/trend`, `GET /supply/demand-sends?date=`, `GET /production/fillers` | `POST /supply/demand/send` | `demand.sent` |
| 7 | `/constraint`| `app/constraint/page.tsx`| `components/constraint/constraint.tsx` | `GET /limits`, `GET /limits/schema`, `GET /plans`, `POST /plans/{id}/check` (preview) | `PUT /limits` | `limits.updated` |
| 8 | `/persetujuan`| `app/persetujuan/page.tsx`| `components/persetujuan/persetujuan.tsx`| `GET /audit?kind=`, `GET /audit.csv` | None (read-only audit ledger) | Any mutation event (`*.decided`, `*.created`, `*.acked`, etc.) |
| 9 | `/trace` | `app/trace/page.tsx` | `components/trace/trace.tsx`, `evidence-graph.tsx`, `genealogy.tsx` | `GET /quality/active-event`, `GET /assistant/templates`, `POST /assistant/ask`, `GET /trace/backward`, `GET /trace/forward`, `GET /trace/explain`, `GET /trace/handoffs`, `GET /lots/locations`, `GET /holds`, `GET /holds/summary`, `GET /graph` | `POST /assistant/ask`, `POST /holds` | `hold.created`, `hold.reviewed` |
| 10 | `/lab` | `app/lab/page.tsx` | `components/lab/lab.tsx` | `GET /lab/results?kind=`, `GET /lab/summary`, `GET /meta` | `POST /lab/results/{id}/decision` | `lab.decided` |
| 11 | `/hold` | `app/hold/page.tsx` | `components/hold/hold.tsx` | `GET /holds`, `GET /holds/summary`, `GET /lots/locations`, `GET /holds/{id}/genealogy` | `POST /holds/{id}/review` | `hold.created`, `hold.reviewed` |
| 12 | `/graph` | `app/graph/page.tsx` | `components/graph/context-graph.tsx` | `GET /graph`, `GET /sources/{id}` | Filter / Search / Focus node (client UI) | `snapshot.updated` |
| 13 | `/sumber-data`| `app/sumber-data/page.tsx`| `components/sumber-data/sumber-data.tsx`| `GET /data-sources` | `POST /data-sources/{id}/test`, `POST /data-sources/{id}/sync` | `source.synced` |
| 14 | `/pengguna` | `app/pengguna/page.tsx` | `components/pengguna/pengguna.tsx` | `GET /roles`, `GET /session` | `POST /session/role` | None |
| Shell | Root Layout | `app/layout.tsx` | `components/shell/app-shell.tsx` & `components/ui/index.tsx` | `GET /meta`, `GET /session`, `GET /shifts`, `GET /notifications`, `GET /sources/{id}` | `POST /session/role`, `POST /demo/reset` | `*` (SSE notification counter and badge) |

---

## 3. Detailed Component Semantics, State Invariants & Cascades

### 3.1 Receiving Flow (`/`)
- **Key Invariant:** Gated State Machine. Road tankers advance through 7 stages: `Farms` (3), `On the way` (4), `Sampling` (2), `Lab test` (3), `Released` (1), `Unloading` (2), `Stored` (3).
- **Mathematical Invariant:** `stages[id].count` must exactly match the number of tankers in `tankers[]` having `stage === stage.label`.
- **Incident Banner:** Highlights `RT-26` (KUD Ngantang, 11,400 L). Milk age is 9h 20m against a strict 10h 30m limit (`[asumsi]`), leaving only 1h 10m holding time.
- **Root Causes:**
  1. Lab bottleneck: Average test queue wait is 52 minutes (14m above baseline).
  2. Bay 2 CIP lock: Bay 2 is in Clean-in-Place until 15:20 (CIP duration 70m started at 14:10).
- **Cascade Behavior:**
  - When the Planner approves the solver recommendation on `/rencana`, the incident banner updates from `"Rencana tidak layak, 3 constraint dilanggar"` to `"Rekomendasi disetujui, RT-26 pindah ke Bay 1"`.
  - When QA releases `RT-27` (`LAB-9031`) on `/lab`, `RT-27` dynamically moves from `Lab test` to `Released`, decrementing the lab queue from 3 to 2.

### 3.2 Rencana & What-if (`/rencana`)
- **Key Invariant:** Dual-plan comparison (`before` FIFO baseline vs. `after` solver recommendation).
- **Horizon Definition:** Fixed operational horizon 13:00 to 20:00 (420 minutes). Snapshot is taken at `14:42` (offset = 102 minutes).
- **Starvation Invariant:** Aseptic Tank `AT-2` feeds both Filling lines `F2` (UHT Strawberry 200ml) and `F4` (UHT Strawberry Kids 105ml). Under FIFO (`before`), `AT-2` empties at `15:50`, causing a 45-minute starvation shutdown across both lines simultaneously (170 min to 215 min).
- **CP-SAT Solver Metrics:**
  - Constraints violated: `before` = 3, `after` = 0.
  - Average RT turnaround time: `before` = 3h 12m, `after` = 2h 41m.
  - Filling starvation: `before` = 45m, `after` = 0m.
  - Yard queue at 16:00: `before` = 5 tankers, `after` = 2 tankers.
- **Workflow State & Role Enforcement:**
  - Only `planner` role has `plan.decide` permission.
  - Approving a plan requires an optional note; rejecting requires a mandatory note (`maxLength: 500`).
  - Approval generates an immutable audit record and emits 6 actionable instructions: 5 for Receiving (tanker reassignment) and 1 for Production Supervisor (`F2/F4` feed continuity).

### 3.3 Configuration Constraints (`/constraint`)
- **Mathematical Specification of the 6 Constraints (`lib/rules.ts`):**
  1. **`flow` (Flow Rate & Starvation):** `FACTS.transfer <= limits.maxTransfer && starved <= limits.starvationTol`. In `before`, UHT-2 transfer is 10,200 L/h (exceeding 9,000 L/h limit) and starvation is 45m (exceeding 20m tolerance).
  2. **`maintenance`:** Verifies no unloading occurs at `Bay 3` between 16:00-16:30 (`MAINTENANCE.start = 180`, `end = 210`) when enabled.
  3. **`holding` (Holding Time Limits):** `limits.holdingMin - (RT26_AGE + fact.finish - NOW) >= 0` AND `FACTS.bufferAge <= limits.bufferMaxMin`. In `before`, RT-26 finishes at 16:15 (offset 195), milk age hits 10h 53m (exceeding 10h 30m / 630 min).
  4. **`cip` (Downtime & CIP Clash):** Tanker unloading must not overlap `[cip.start, cip.end + limits.cipBuffer]`. In `before`, RT-26 is scheduled at Bay 2 at 14:50 (offset 110), clashing directly with CIP Bay 2 (ends at 15:20, offset 140).
  5. **`tank` (Raw Milk Buffer Capacity):** `FACTS.tankPeak <= limits.tankMax`. Buffer peak in `before` is 91%, in `after` is 94%, both within default 95%.
  6. **`changeover` (SOP Matrix Compliance):** Verifies F1 size changeover (1 L to 200 ml) satisfies minimum SOP duration (35 minutes) and total changeovers per line <= `limits.changeoverMax` (2).
- **Live Draft State:** Edits to sliders immediately trigger live re-evaluation of all 4 plan variations (`current:before`, `current:after`, `cip-molor:before`, `cip-molor:after`). Saving writes a formatted change log (e.g., `"Farm sampai tersimpan dingin 10j 30m → 10j 20m"`).

### 3.4 3D Plant Simulation (`/simulasi` & `plant-scene.tsx`)
- **Visual Canvas:** Built with Three.js (`0.170.0`) rendering a realistic isometric model of the plant traced from satellite coordinates (`origin: -7.8683, 112.5187`).
- **Scene Objects:** Main processing hall, raw milk silos (RMT-1..3), receiving bays (Bay 1..3), internal process zones (Degassing, Thermal, Separator, Storage, Mixing, Sterilization, Aseptic, Filling), yard queue, weighbridge, IPAL wastewater plant, parking, and incoming tanker routes.
- **View Toggle & Camera Focus:**
  - Views: `before` (Initial Plan @ 14:42) vs `after` (Recommendation projected @ 15:30).
  - Focus viewpoints (`FOCI`): `overview`, `lab`, `yard`, `bay1`, `bay2`, `silos`, `f2`.
  - Dynamic state: In `before`, Bay 2 shows animated pulsating CIP warning rings, and lines `F2`/`F4` glow red denoting starvation. In `after`, Bay 2 clears, silos rebalance, and lines glow calm green.

### 3.5 Traceability, Assistant & Genealogy (`/trace`, `evidence-graph.tsx`, `genealogy.tsx`)
- **Trigger Event:** QC FG out-of-spec notification: Lot `FG-1013-07` (UHT Chocolate 200ml) tested positive for beta-lactam antibiotic residue on Oct 13 at 22:40 (`LAB-8812`).
- **Natural Language Assistant Templates:**
  - `trace_backward(lot_id = FG-1013-07)`: Traces upstream ingredients through Aseptic Tank AT-2, Mixing MX-2, Storage ST-4, Separator SP-1, Thermal TH-1 to Raw Milk Tank RMT-2. Identifies 4 constituent tankers: RT-02 (31%), RT-04 (12%), RT-06 (27%), RT-09 (30%). Flagged suspect: `RT-04` (Farm Ngadirejo, screening rapid test passed but near detection limit at 06:05).
  - `trace_forward(rt_id = RT-04, tanggal = 13 Okt)`: Traces contaminated milk forward. Proves that instead of holding all 14 lots (101,200 packs) produced from RMT-2, only 5 specific lots from Filling Line F2 contain milk from RT-04 (total 36,100 packs).
  - `explain_resource(resource = Bay 2, waktu = 14.50)`: Explains CIP conflict blocking RT-26.
- **Handoff Chain (Batch Lineage):** 7 transfer steps from weighbridge to finished carton. Specifically highlights 2 critical documentation gaps (`gap: true`):
  1. `SP-1` separator to `ST-4` storage: Recorded only in manual operator log; MilkGraph reconstructs this by intersecting SP-1 flowmeter timestamps with ST-4 level rise.
  2. `ST-4` to `MX-2` premix mixing: Matches MES batch record with ST-4 level drop.
- **Recall vs. Hold Invariant:**
  - Lots located in warehouse (`FG-1013-06`, `FG-1013-07`, `FG-1013-08`, `FG-1013-09`) are classified as internal **Hold**.
  - Lot `FG-1013-05` has already been dispatched to `DC Sidoarjo` (`shipped: true`), triggering a mandatory customer **Recall** action.

### 3.6 Hold Management (`/hold`)
- **State Lifecycle:** `diajukan` (Submitted) -> `aktif` (Active/Approved by QA) | `ditolak` (Rejected by QA) -> `dilepas` (Released after reinspection).
- **Permissions:** `hold.submit` (Planner, Supervisor, QA); `hold.review` (Strictly QA).
- **Printable Genealogy Report:** Generates a structured audit report with full batch handoffs, lab certificates (`LAB-4471`, `LAB-8812`), and inventory locations.

### 3.7 Lab Operations (`/lab`)
- **Queue Gating:** 3 tankers in queue: `RT-24` (repeat test pending alcohol test clarification, ETA 15:30), `RT-27` (tested, ready for QA decision), `RT-28` (testing).
- **Decision Invariant:** QA can release or reject tankers. Releasing moves the tanker to `Released`. Tankers in `uji-ulang` cannot be released (API must return HTTP 422).

### 3.8 Supply & Demand Integration (`/pasokan`)
- **Mass Balance:**
  - Total Finished Goods Demand: 1,060,000 packs across 10 SKUs.
  - Raw Milk Required: Calculated per SKU volume / `(1 - processLoss)` where `processLoss = 0.03` (3%) -> Exactly 278,659 L.
  - Farm Commitments: 310,800 L across 6 farms. Daily plant capacity: 320 tons (310,680 L @ density 1.03 kg/L).
- **Send Demand Action:** One-way dispatch of daily production quotas to suppliers (`demand.send` permission). Prevents duplicate dispatch on same date (HTTP 409).

### 3.9 Audit Trail (`/persetujuan`)
- **Append-Only Immutable Ledger:** Records every operational action across the plant (`rencana`, `hold`, `lab`, `constraint`, `instruksi`).
- **CSV Export:** Client-side and server-side export formatted with UTF-8 BOM (`﻿`) and Indonesian semicolons/commas for clean Excel import.

---

## 4. Documentation Contradictions & Outdated Specifications

During our systematic review of `docs/01-PRD.md` through `docs/08-DESIGN.md`, several critical contradictions and architectural drifts were uncovered:

### 4.1 CRITICAL: Backend Stack Override (FastAPI vs. Go REST)
- **Documented Specification:** All existing architectural documents (`01-PRD`, `02-PLAN`, `03-ARCHITECTURE`, `04-API-CONTRACT`, `05-INTEGRASI-FE-BE`) explicitly prescribe **Python FastAPI**, Pydantic v2, Uvicorn, and Python-native packages (`ortools.constraint_solver`, `simpy`, `langchain/langgraph`).
- **Active User Requirement:** The user has explicitly overridden the backend requirement to a **high-performance Go REST backend** (`Milkgraph-Back`) backed by PostgreSQL.
- **Impact & Necessary Adjustments:**
  1. The API contract (`04-API-CONTRACT.md`) must be treated as a language-agnostic HTTP/JSON specification.
  2. Data models must be implemented as Go structs with `json:"camelCase"` tags.
  3. The Rules Engine (`lib/rules.ts`) must be ported directly to Go standard library algorithms without external Python dependencies.
  4. Scheduling optimization must either use a Go heuristic scheduler, Go OR-Tools CGO bindings, or an isolated scheduling solver service.

### 4.2 Outdated Bootstrap Prompts (`docs/PROMPT-CODEX.md`)
- `docs/PROMPT-CODEX.md` is an uncurated archive from the project's inception. It references outdated component structures, temporary ports, and early draft assumptions that are completely superseded by `01-PRD.md` and the actual Next.js 16 implementation.

### 4.3 Missing Fields in Initial API Contracts vs. Actual Frontend Needs
As identified in `05-INTEGRASI-FE-BE.md` §2.8 and verified against frontend TS components, the backend must support several additional fields not fully specified in the earliest drafts of `04-API-CONTRACT.md`:
1. `Answer.subjectRt` (string): Needed by `/trace` and `/hold` to dynamically pass `"RT-04"` without hardcoding.
2. `ReceivingView.incident.summary` (string): Dynamic incident description text.
3. `LabResult.eta` (string): Expected completion time for retests (`"15.30"` for RT-24).
4. `SiteStatus.viewLabel` (string): Toggle label text for 3D simulation (`"Rencana awal · 14.42"` vs `"Rekomendasi · proyeksi 15.30"`).
5. `GET /assets/cip-policy`: Returns standard tank CIP interval (`{ tankIntervalHours: 24 }`).

### 4.4 Hardcoded Text Strings Requiring API Externalization
The frontend currently has several static strings in component JSX that must be served dynamically once live mode is connected:
- `components/receiving/receiving.tsx`: Incident factors, lab waiting averages, holding limit footer text.
- `components/rencana/rencana.tsx`: Gantt resource labels, timeline horizon strings.
- `components/tangki/tangki.tsx`: Tank capacity strings, CIP warnings.
- `components/constraint/constraint.tsx`: Parameter descriptions and step ranges (to be provided via `/limits/schema`).

### 4.5 Domain Assumptions (`[asumsi]` and `[simulasi]`)
Throughout both UI and documentation, dozens of operational parameters are tagged with `[asumsi]` (assumptions pending industrial mentor validation) or `[simulasi]` (synthetic demo data). These include:
- Maximum holding time: 10h 30m (630 min).
- Raw milk buffer maximum storage before thermalization: 18h (1,080 min).
- CIP Bay duration: 70 min; CIP tank interval: 24 hours.
- Maximum buffer tank level threshold: 95%.
- Changeover duration matrix: 25 to 50 minutes depending on package size/flavor.

---

## 5. Exact Go REST API Requirements for `Milkgraph-Back`

To provide seamless, drop-in support for `Milkgraph-Front`, `Milkgraph-Back` must implement the following 35 endpoints grouped by domain:

### 5.1 System, Session & Shell Endpoints
- `GET /api/v1/meta`: Returns system mode (`"demo"` | `"live"`), timestamp `"14.42"`, date `"Rabu, 14 Oktober 2026"`, and Gantt horizon offset (`102`).
- `GET /api/v1/session`: Current active user profile, role, and permission array.
- `POST /api/v1/session/role`: Switches active demo role (`planner`, `receiving`, `supervisor`, `qa`).
- `GET /api/v1/roles`: Full RBAC role definition matrix and permission labels.
- `GET /api/v1/shifts`: Shift calendar list (`Shift Siang`, `Shift Malam`, `Shift Pagi`).
- `GET /api/v1/notifications`: Active operational alerts (Plan failure, RT-26 holding, QC FG out-of-spec).
- `GET /api/v1/sources`: Full repository of evidence cards (Weighbridge tickets, lab logs, flowmeter records).
- `GET /api/v1/sources/{id}`: Single evidence card detail modal.
- `POST /api/v1/demo/reset`: Resets database state to initial demo baseline.

### 5.2 Receiving Flow Endpoints
- `GET /api/v1/receiving`: Consolidated receiving view payload:
  - `metrics`: 4 cards (Throughput, Turnaround, Lab Queue, Active Bays) with sparkline history arrays.
  - `stages`: 7 lifecycle stages with exact tanker counts.
  - `tankers`: 8 road tanker records currently in plant.
  - `assets`: 3 receiving bays and 3 raw milk buffers (RMT-1..3).
  - `labHistory`: 6-hour hourly lab queue progression.
  - `labSummary`: Average wait time and pending tanker count.
  - `incident`: Active incident card data (RT-26 critical holding time).

### 5.3 Plant Assets & Filling Production Endpoints
- `GET /api/v1/assets`: Real-time status, volume, capacity, and temperature of Bay 1..3 and RMT 1..3.
- `GET /api/v1/assets/cip-log`: Recent CIP cleaning cycle history across bays and tanks.
- `GET /api/v1/assets/cip-policy`: Preventive cleaning intervals.
- `GET /api/v1/assets/mid-tanks`: Intermediate tanks (Standardization ST-1..6, Cream CT-1..3, Premix PM-3, Mixing MX-1..2).
- `GET /api/v1/production/aseptic-tanks`: Aseptic storage tanks AT-1..5 with inflow/outflow rates.
- `GET /api/v1/production/fillers`: Packaging lines F1..F10 with run speeds, active SKU, and status.
- `GET /api/v1/production/unit-states`: Visual status dictionary (jalan, standby, CIP, sanitasi, perbaikan).
- `GET /api/v1/production/changeover-matrix`: Matrix of standard changeover durations between products.
- `GET /api/v1/plans/{planId}/instructions`: Generated execution instructions resulting from approved schedule.
- `POST /api/v1/instructions/{id}/ack`: Role-enforced operational acknowledgment of an instruction.

### 5.4 Planning, Solver & Constraints Endpoints
- `GET /api/v1/plans`: List of operational schedules (`current` FIFO vs `cip-molor` What-if).
- `GET /api/v1/plans/{planId}`: Schedule detail with complete Gantt before/after block arrays.
- `POST /api/v1/plans/{planId}/check`: Evaluates the 6 operational constraints against stored or draft limits.
- `POST /api/v1/plans/{planId}/solve`: Triggers schedule optimization under modified constraint parameters.
- `GET /api/v1/decisions`: History of human approvals/rejections for plans.
- `POST /api/v1/plans/{planId}/decision`: Submits planner approval or rejection with audit logging.
- `GET /api/v1/limits`: Current active threshold limits.
- `GET /api/v1/limits/schema`: UI schema defining sliders, toggles, units, and ranges for the `/constraint` screen.
- `PUT /api/v1/limits`: Updates operational limits and writes constraint audit record.

### 5.5 Traceability, Lab & Hold Endpoints
- `GET /api/v1/quality/active-event`: Active QC incident triggering traceability (`FG-1013-07`).
- `GET /api/v1/assistant/templates`: Quick-action template questions for graph querying.
- `POST /api/v1/assistant/ask`: Natural language query router to structured graph query templates.
- `GET /api/v1/trace/backward`: Upstream trace resolving raw milk sources and mixing proportions.
- `GET /api/v1/trace/forward`: Downstream trace resolving affected finished product lots and package counts.
- `GET /api/v1/trace/explain`: Resource clash explanation (Bay 2 CIP overlap).
- `GET /api/v1/trace/handoffs`: Complete custody transfer chain with documentation gap indicators.
- `GET /api/v1/lots/locations`: Warehouse storage vs. dispatch location (identifying recall status).
- `GET /api/v1/holds`: Registry of active and past product quarantine holds.
- `POST /api/v1/holds`: Creates a new product hold quarantine order.
- `POST /api/v1/holds/{id}/review`: QA review action (approve hold, reject hold, release hold).
- `GET /api/v1/holds/{id}/genealogy`: Detailed genealogy dossier for printable reporting.
- `GET /api/v1/lab/results`: Incoming tanker milk lab tests and finished good release testing.
- `GET /api/v1/lab/summary`: Waiting vs released lab metrics.
- `POST /api/v1/lab/results/{id}/decision`: QA release or rejection of incoming tanker.

### 5.6 3D Simulation, Context Graph & Realtime SSE
- `GET /api/v1/site/layout`: Scaled 3D spatial layout coordinates for Three.js rendering.
- `GET /api/v1/site/status`: Dynamic equipment and yard statuses parameterized by `?view=before|after`.
- `GET /api/v1/site/bottlenecks`: Top operational bottlenecks parameterized by `?view=before|after`.
- `GET /api/v1/graph`: Complete topological bipartite knowledge graph (nodes, edges, ranks).
- `GET /api/v1/data-sources`: Configuration and health of external industrial bridges.
- `POST /api/v1/data-sources/{id}/test`: Connection probe test with latency measurement.
- `POST /api/v1/data-sources/{id}/sync`: Manual sync refresh trigger.
- `GET /api/v1/audit`: Queryable audit log with filtering by event kind.
- `GET /api/v1/audit.csv`: Formatted CSV stream with UTF-8 BOM.
- `GET /api/v1/events`: Server-Sent Events (`text/event-stream`) broadcasting real-time system changes.

---

## 6. Implementation Architecture for `Milkgraph-Back` (Go)

### 6.1 Recommended Go Package Layout
```
Milkgraph-Back/
├── cmd/
│   └── server/
│       └── main.go              # HTTP server entrypoint, graceful shutdown
├── internal/
│   ├── api/
│   │   ├── router.go            # Chi or stdlib net/http route definitions
│   │   ├── middleware.go        # RBAC, X-Demo-Role extractor, CORS, audit
│   │   └── handlers/            # HTTP handlers per domain
│   ├── domain/                  # Pure domain types matching TS models
│   │   ├── models.go            # Tanker, Asset, Plan, Lot, Hold, etc.
│   │   └── errors.go            # Standard error envelopes
│   ├── rules/
│   │   └── engine.go            # Port of lib/rules.ts (6 constraints)
│   ├── trace/
│   │   └── genealogy.go         # Dynamic mix & forward lot propagation
│   ├── store/
│   │   ├── postgres/            # PostgreSQL repository implementations
│   │   └── memory/              # Seed fixture loader and memory cache
│   └── sse/
│       └── broker.go            # Multi-client event broadcaster
└── seed/
    └── *.json                   # Seed fixtures mirrored from frontend
```

### 6.2 Data Invariants & Rules Engine Verification
1. **Golden Rules Exactness:**  
   The Go port of `lib/rules.ts` must pass the exact 9 test assertions defined in `mock/fixtures/golden-rules.json`. Indonesian text strings, unit formatting (e.g., `10.200 L/jam`, `3,8 °C`), and punctuation must match character-for-character.
2. **Genealogy Mass Balance Invariant:**  
   The sum of constituent raw milk percentages in backward trace must equal exactly 100% (`RT-04 12% + RT-02 31% + RT-06 27% + RT-09 30% = 100%`). The forward trace must assign the exact identical percentage to the trigger lot (`FG-1013-07` carries `12%`).
3. **Audit Log Immutability:**  
   Every write operation (`POST` / `PUT`) must create an append-only `LogEntry` with incrementing monotonic ID, real server timestamp formatted as Indonesian `"HH.MM"`, actor name, role, and tone.

---

## 7. High-Risk Findings & Strategic Technical Advice for Parent Agent

1. **High-Risk 1: Rules Engine String Matching**  
   The frontend displays constraint check strings directly without local formatting. Any discrepancy in decimal comma (`3,8` vs `3.8`), thousand separator (`10.200` vs `10200`), or wording will cause automated regression tests (`rules.test.mjs`) to fail.  
   *Mitigation:* Create a dedicated test in Go asserting exact JSON equality against `golden-rules.json`.

2. **High-Risk 2: Genealogy Calculation vs. Scenario Fixture**  
   In a live plant, genealogy requires complex time-window flow integration. For the demo scenario, the numbers are calibrated to match the specific bootcamp case.  
   *Mitigation:* Implement a hybrid genealogy service in Go that serves verified fixture numbers for historical demo IDs (`FG-1013-07`, `RT-04`) while supporting algorithmic propagation for newly injected batches.

3. **High-Risk 3: Frontend Client Integration Pattern**  
   `Milkgraph-Front` currently imports mock data synchronously (`import { scenario } from "@/lib/data"`).  
   *Mitigation:* In `05-INTEGRASI-FE-BE.md`, the recommended pattern is to preserve `lib/demo.ts` interface while backing it with `useResource()` hooks. Do NOT refactor frontend components directly until the Go backend serves all M1 fixture endpoints reliably.

4. **High-Risk 4: Real-time Multi-Tab Coordination (SSE)**  
   The user demo script requires two browser tabs to remain synchronized in real time (e.g. approving a plan in Tab A instantly updates the receiving incident banner in Tab B).  
   *Mitigation:* Implement a thread-safe SSE broker in Go (`internal/sse/broker.go`) using standard Go channels to broadcast domain event payloads (`plan.decided`, `hold.created`, `lab.decided`, `limits.updated`).

5. **High-Risk 5: Time & Horizon Representation**  
   The demo relies on a fixed historical simulation time (`14:42 WIB`, horizon `13:00` to `20:00`, offset `102`).  
   *Mitigation:* All duration and clock calculations must preserve the 102-minute baseline offset rather than anchoring to the physical host's system clock.
