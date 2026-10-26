---
id: "01-attribute-contention"
title: "Attribute local SQLite writer contention"
status: completed
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-PLATFORM-POSTGRES-DOMAIN-STORE-PARITY-007
acceptance_criteria:
  - AC-PLATFORM-POSTGRES-DOMAIN-STORE-PARITY-007.1
  - AC-PLATFORM-POSTGRES-DOMAIN-STORE-PARITY-007.3
  - AC-PLATFORM-POSTGRES-DOMAIN-STORE-PARITY-007.4
  - AC-PLATFORM-POSTGRES-DOMAIN-STORE-PARITY-007.6
system_design:
  - ../../specs/platform/system-design/git-credential-lease-reissue.md
  - ../../specs/platform/system-design/postgres-domain-store-parity.md
---

# Task 01: Attribute local SQLite writer contention

## Summary

Establish a reproducible local workload and identify the writer occupancy that
causes probe failure. Do not treat the incident's correlated retention logs as
causal evidence. Obtain a Go 1.26-compatible toolchain before running checks.

## In scope

- New `TestCredentialContentionLoad` in backendapp, using a disposable SQLite
  database with real separate factory pools and real persistence middleware.
- Seed valid task/session/repository ownership and synthetic credentials; use
  delivery receipt/projection methods, not only a transaction artificially held open.
- Exercise 1/4/8 writers, short and long transactions, retention enabled/disabled,
  idle control and about 2663 retained candidates. Use bounded local runs.
- Measure per-operation checkout delay separately from held transaction time,
  count commits/projections and capture probe stages and pool counter deltas.
- Read the exact long-transaction/high-rate methods identified by the fixture;
  test a disposable optimization only after attribution. Record data in this
  package's `evidence.md`, with exact commands and limitations.

## Out of scope

Production load, arbitrary tuning, weakening failure policy, and unmeasured
production optimizations. Existing completed contention/batching plans retain
their historical results and are not reopened.

## Acceptance

1. The local fixture identifies a producer and reproduces the gate-to-helper
   failure, or explicitly records that attribution remains unresolved. An
   unresolved result blocks claims that the broader starvation issue is fixed.
2. Fault controls retain writer denial, closed pool, missing tables, disposable
   corrupt file, maintenance deferral and subsequent recovery; no credential is
   returned while the persistence gate is closed.
3. Verify delivery cursor monotonicity, duplicates/effects, payload hashes and
   reopen integrity. Any proposed producer correction must improve the same
   workload without changing receipt/cleanup guarantees.

## Verification

From the repository root, after adding the named regression:

```bash
(cd apps/backend && go test -trimpath -tags fts5 ./internal/persistence/requiredstores -run '^TestPersistenceContentionFixture$' -count=1 -v)
(cd apps/backend && go test -trimpath -tags fts5 ./internal/backendapp -run '^TestCredentialContentionLoad$' -count=1 -v -timeout=5m)
(cd apps/backend && go test -trimpath -tags fts5 -race ./internal/backendapp -run '^TestCredentialContentionLoad$' -count=1 -timeout=10m)
```

## Files likely touched

- `apps/backend/internal/backendapp/credential_contention_test.go` (new)
- `docs/plans/git-credential-writer-contention/evidence.md` (new)
- Read: `internal/persistence/requiredstores/health.go`, `internal/db/sqlite.go`,
  `internal/task/repository/sqlite/agent_delivery.go`,
  `internal/task/repository/sqlite/agent_delivery_retention.go`, and
  `internal/task/deliveryretention/worker.go`, all under `apps/backend`.

## Dependencies

None. Toolchain availability is a prerequisite.

## Risks

A held pool connection proves the failure mechanism but not a realistic producer.
Do not use timing-sensitive performance assertions as correctness gates.
An unavailable PostgreSQL DSN is a coverage limitation, not a passing result.

## Parallelism

`sequential`

## Inputs

Plan evidence and constraints; required-store design and referenced ADRs;
existing `health_contention_test.go` and retention worker integration fixtures.

## Results

Completed with the attribution limitation recorded in [evidence.md](evidence.md).
The deterministic regression proves the redundant writer checkout; the full
six-case race workload and fault/integrity controls pass. Historical producer
attribution remains unresolved; storage latency was separately repaired by the operator.
