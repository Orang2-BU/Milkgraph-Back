# Greenfields Master Data v2.1

## Scope

`seed/master/` contains the 13 client-approved CSV files from `Greenfields_Ideation_Package_v2_1`. The server embeds them at build time, validates their schema and relationships on startup, then exposes a read-only API.

This is a master topology and catalog baseline. It is not a record of actual plant transfers, batches, production runs, inventory, or CIP events.

## Read-only endpoints

All require normal API authentication.

| Endpoint | Data |
|---|---|
| `GET /api/v1/master` | Version, validation status, row counts, planning readiness |
| `GET /api/v1/master/assets` | 73 configured assets |
| `GET /api/v1/master/connections` | 118 allowed connections |
| `GET /api/v1/master/products` | Liquid products, pack sizes, market variants |
| `GET /api/v1/master/finished-skus` | Finished SKU catalog |
| `GET /api/v1/master/lines` | Line dedication and compatibility whitelists |
| `GET /api/v1/master/rules` | Changeover, CIP, constraint, unit catalogs |

## Validation

Startup validation rejects:

- missing/duplicate catalog IDs;
- connection source, destination, or `via_asset_id` outside the asset catalog;
- `via_asset_id` that is not a Dedicated Pipe;
- filling-line and aseptic-tank dedication that does not match an `aseptic_supply` connection;
- product, pack, market, finished SKU, and line compatibility references that do not resolve;
- finished-SKU rows whose product/pack/market projection differs from the canonical catalog;
- incomplete finished-SKU eligibility generated from the line-product compatibility catalog.

`line_dedication` is a summary. Finished-product scheduling authority is `line_finished_sku_compatibility`; code must not invent a cartesian product from the summary fields.

## Data limits

`X`, `N/A`, and blank values retain their original distinct meanings. They are never coerced to zero or legacy fixture defaults.

`numericalPlanningReady` is `false` because capacity, flow, speed, duration, and holding parameters are placeholders. The existing solver, traceability, graph, and operational UI remain legacy demo behavior until later stages replace them with versioned master data plus real operational events.

`asset_connections` describes allowed routes only. It is not transfer evidence and cannot create a genealogy or mass balance without timestamped batch-transfer records.
