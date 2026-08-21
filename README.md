# Shenzhen Light Governance

Shenzhen Light Governance is a backend service for controlling spill light from public sports facilities. It gives residents, venue operators, urban-management reviewers, lighting assessors, and maintenance crews one auditable workflow from a complaint through professional measurement, rectification, inspection, resident follow-up, and closure.

The reference deployment models the Cuihu Sports Park football field in Luohu. It records balcony and window-side illuminance evidence, assigns limited assessment capacity, isolates districts and lighting zones, tracks shields and fixture-angle work, enforces the 22:30 cutoff, and rejects closure until a rectification has been independently reviewed.

- Language: Go 1.26
- Default port: 49820
- Storage: SQLite relational index plus JSONL event shards
- Identity: expiring server-side sessions with logout revocation and role checks

## Business Workflows

- Complaint intake preserves the resident's original evidence and emits an audit event.
- Professional assessment reserves measurement capacity with idempotency and optimistic versions.
- Lighting-zone assignment checks district ownership, venue type, active status, and fixture capacity.
- Operating rules apply effective windows and capacity limits, including the nightly cutoff boundary.
- Rectification review requires before/after measurements and work evidence before acceptance.
- Assessor panels maintain bounded membership and isolated milestone data.
- Filters, pagination, worker retries, context cancellation, and restart recovery are covered by tests.

## Layout

```text
cmd/lightgovernance/  HTTP service, graceful shutdown, health and readiness
cmd/hubctl/           migration, import, export, index rebuild and diagnostics
internal/auth/        password login, session expiry, revocation and role checks
internal/hub/         light complaints, assessment, zones, rules and rectification
internal/service/     transactional cross-entity workflows
internal/repo/        SQLite and JSONL persistence with restart recovery
internal/httpapi/     JSON API, request IDs and stable error mapping
internal/scheduler/   deadline escalation and retry policy
internal/worker/      cancellable background reevaluation
migrations/           versioned relational schema
```

## Run

```bash
go build ./...
go run ./cmd/hubctl init -data-dir ./data
SHENZHEN_LIGHT_GOVERNANCE_AUTH_BOOTSTRAP_USERS='[{"id":"u-admin","username":"admin","password":"change-this-password","role":"admin"},{"id":"u-reviewer","username":"reviewer","password":"change-this-too","role":"urban_management_reviewer"}]' \
  go run ./cmd/lightgovernance config.example.yaml
```

Use `GET /healthz` for liveness and `GET /readyz` for dependency readiness. Authenticated APIs expose complaints, rules, assessment batches, audit events, and failed background operations.

## Verification

```bash
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./...
```

The root Dockerfile builds the real `cmd/lightgovernance` and `cmd/hubctl` entrypoints with the Go version declared in `go.mod`.
