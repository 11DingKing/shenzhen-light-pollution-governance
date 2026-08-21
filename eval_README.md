# CultureCamp Eval — Government Service Lead-Responsibility Adjudication

## Purpose
Go backend service that adjudicates a unique lead agency for every
cross-agency government service item at registration time, tracks rule
versions, handles deadline-based escalation, and provides batch import/export,
reconciliation, and diagnostics tooling.

## Data Directory
All persistent state lives under a configurable data directory (default `./data`).
The directory contains:
- `shards/` — JSONL shard files sharded by date, one sub-tree per entity type
  (`items`, `rules`, `assignments`, `escalations`, `audit`, `batches`)
- `index.db` — embedded SQLite index (acceleration + manifest)
- `schema_version` table tracks applied migrations

## Standard Commands
```bash
go build ./...
go vet ./...
go run ./cmd/lightgovernance        # start HTTP service
go run ./cmd/hubctl init --data-dir ./data   # initialise storage
go test ./...
```

## Running the Service (port 49820)
```bash
export SHENZHEN_LIGHT_GOVERNANCE_SERVER_PORT=49820
export SHENZHEN_LIGHT_GOVERNANCE_STORAGE_DATA_DIR=./data
go run ./cmd/lightgovernance
# Health:   curl http://localhost:49820/healthz
# Readiness: curl http://localhost:49820/readyz
```

## CLI Tool
```bash
go run ./cmd/hubctl init      --data-dir ./data
go run ./cmd/hubctl import    --data-dir ./data --file items.jsonl
go run ./cmd/hubctl export    --data-dir ./data --from 2026-08-01 --to 2026-08-31 --out export.jsonl
go run ./cmd/hubctl reconcile --data-dir ./data
go run ./cmd/hubctl rebuild-index --data-dir ./data
go run ./cmd/hubctl diagnose  --data-dir ./data
```

## Docker Builds
```bash
./build_eval_docker.sh lightgovernance-eval linux/amd64
./build_eval_docker.sh lightgovernance-eval linux/arm64
```
