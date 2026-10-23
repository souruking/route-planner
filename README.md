# Route Planner API

A Go service for vehicle routing problems (CVRP): clients submit a depot, delivery stops with demands, and a fleet of vehicles with capacities. Solving runs as async jobs (in progress).

Built with the standard library `net/http` (Go 1.22+ routing), `pgx` for Postgres, and `goose` for migrations. No web framework.

## Status

- [x] **Part 1: Problems CRUD** with validation, migrations, and tests
- [ ] Part 2: Solve jobs (nearest neighbor + 2-opt)
- [ ] Part 3: Background workers (`FOR UPDATE SKIP LOCKED`, leases)
- [ ] Part 4: Timeouts, cancellation, idempotency keys

## Running locally

Requires Go 1.22+ and Docker.

```bash
docker compose up -d
cp .env.example .env          # DB_URL, HTTP_ADDR, RUN_MIGRATIONS=true
go run ./cmd/api
```

| Variable | Required | Description |
|---|---|---|
| `DB_URL` | yes | Postgres connection string |
| `HTTP_ADDR` | yes | Listen address, e.g. `:8080` |
| `RUN_MIGRATIONS` | no | Apply embedded migrations on startup (default `false`) |

## API

### Create a problem

```bash
curl -i -X POST localhost:8080/problems \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "Almaty morning run",
    "depot": {"lat": 43.238, "lon": 76.945},
    "vehicles": [{"id": "van-1", "capacity": 40}],
    "stops": [{"id": "order-1042", "lat": 43.256, "lon": 76.928, "demand": 5}]
  }'
```

```
HTTP/1.1 201 Created
Location: /problems/48add3a5-c85a-40b3-aa87-a8f4a4bcbb88

{"id":"48add3a5-c85a-40b3-aa87-a8f4a4bcbb88","name":"Almaty morning run",
 "depot":{"lat":43.238,"lon":76.945},
 "vehicles":[{"id":"van-1","capacity":40}],
 "stops":[{"id":"order-1042","lat":43.256,"lon":76.928,"demand":5}],
 "created_at":"2026-10-23T17:44:06.72467Z"}
```

The `id` on stops and vehicles is the client's own identifier and must be unique within the problem.

### Get a problem

```bash
curl localhost:8080/problems/48add3a5-c85a-40b3-aa87-a8f4a4bcbb88
```

Stops and vehicles come back in the order they were submitted.

### List problems

```bash
curl 'localhost:8080/problems?limit=2'
```

```json
{
  "items": [
    {"id": "5cc81209-...", "name": "run 3", "created_at": "2026-10-23T17:44:06.749545Z"},
    {"id": "3e0c2f70-...", "name": "run 2", "created_at": "2026-10-23T17:44:06.738301Z"}
  ],
  "next_cursor": "eyJ0IjoiMjAyNi0xMC0yM1QxNzo0NDowNi43MzgzMDFaIi..."
}
```

Newest first. Pass `next_cursor` back as `?cursor=` for the next page; it is `null` on the last page. `limit` defaults to 20, maximum 100.

### Delete a problem

```bash
curl -i -X DELETE localhost:8080/problems/48add3a5-c85a-40b3-aa87-a8f4a4bcbb88
# HTTP/1.1 204 No Content
```

### Health

| Endpoint | Meaning |
|---|---|
| `GET /healthz` | Process is alive. Never touches the database. |
| `GET /readyz` | Database is reachable; returns 503 otherwise. |

## Errors

Every error has the same shape:

```json
{"error": {
  "code": "validation_failed",
  "message": "request is invalid",
  "fields": [
    {"field": "stops[0].demand", "message": "15 exceeds the largest vehicle capacity (10)"},
    {"field": "vehicles[1].id", "message": "duplicates vehicles[0].id (\"van-1\")"}
  ],
  "request_id": "75c11dc4209e2691"
}}
```

| Status | Code | When |
|---|---|---|
| 400 | `invalid_json` | Malformed JSON, wrong types, unknown fields, trailing data |
| 400 | `invalid_parameter` | Bad `limit` or `cursor` |
| 404 | `not_found` | Unknown or malformed problem id |
| 413 | `payload_too_large` | Body over 1 MB |
| 415 | `unsupported_media_type` | `Content-Type` is not `application/json` |
| 422 | `validation_failed` | Input breaks a rule; all violations are listed |
| 500 | `internal_error` | Unexpected failure; details are logged, never returned |

`request_id` matches the `X-Request-ID` response header and the server logs.

### Validation rules

- 1–1000 stops, 1–50 vehicles; name up to 200 characters
- Latitude in [-90, 90], longitude in [-180, 180]; missing coordinates are rejected, not treated as 0
- Demand and capacity are positive integers up to 1,000,000
- Every stop's demand fits the largest vehicle, and total demand fits total fleet capacity

The last two are necessary but not sufficient for a solution to exist (that is a bin-packing problem); the solver handles the rest.

## Design notes

- **Layering.** `domain` holds types and rules with no HTTP or DB imports. `httpapi` and `store` both depend on it, never on each other's internals. Handlers see `store.ErrNotFound`, never `pgx.ErrNoRows`.
- **Atomic creates.** A problem and its stops and vehicles are inserted in one transaction. Stops and vehicles use `COPY`, so 1000 stops is one round trip.
- **Consistent reads.** `GET /problems/{id}` runs its three queries in one `REPEATABLE READ` snapshot, so a concurrent delete can't return a problem with no stops.
- **Keyset pagination.** `WHERE (created_at, id) < ($1, $2)` uses the `(created_at DESC, id DESC)` index, so deep pages are as fast as the first. The id breaks ties between problems created in the same microsecond. The cursor is opaque base64 so its format can change.
- **One place for errors.** Handlers return errors; a single function maps them to status codes and hides internal details.
- **Operational basics.** Request IDs, structured `slog` logs, panic recovery, server timeouts, and graceful shutdown on SIGTERM.

## Tests

```bash
go test ./...                    # unit tests; DB tests are skipped
make test-integration            # also runs store and end-to-end tests against Postgres
```

Integration tests need a dedicated database (they truncate tables):

```bash
docker compose exec db createdb -U postgres apidb_test
```

Handler tests use an in-memory fake store; the store's SQL is tested against real Postgres, including transaction rollback on failure and pagination when every row has the same timestamp.

## Project layout

```
cmd/api/              wiring: config, pool, migrations, HTTP server
internal/domain/      types and validation rules
internal/httpapi/     routes, handlers, middleware, error mapping
internal/store/       Postgres queries
internal/migrations/  embedded SQL migrations
```