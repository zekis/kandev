---
created: 2026-10-26
status: in_progress
requirements:
  - REQ-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-001
  - REQ-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-002
  - REQ-PLATFORM-POSTGRES-DOMAIN-STORE-PARITY-007
system_design:
  - ../../specs/platform/system-design/git-credential-lease-reissue.md
  - ../../specs/platform/system-design/postgres-domain-store-parity.md
legacy_specs: []
---

# Git credential acquisition under SQLite writer contention

## Overview

Repair credential acquisition after transient required-persistence rejection,
while retaining fail-closed database and credential authorization checks.
First measure real local writer producers; then implement bounded acquisition
recovery and prove the complete path. Work is sequential in this session.

## Evidence and limits

Base and remote `zekis/kandev:main` were both
`28301f9721b07c2b18948f069dea200357d0a7d4` at inspection. Reuse the clean task
branch `feature/fix-credential-failu-voa`. The current task has no linked PR.

User-reported incident: tm-lab-kandev, October 26 00:55-01:34 UTC, concurrent
agents; credential helper 503 blocked task 9c738526-8f2c-48e4-b031-fb63c0000b7f
publishing ba29b42e. Probe failures were writer_ping/table_probe deadline
exceeded. Writer pool had one connection in use, reader pool four idle;
writer wait count grew about 19771 to 32517 and cumulative wait milliseconds
about 2.57 million to 11.91 million. Healthy service and normal helper push
returned after load ended. Retention snapshots correlated with the incident
but do not attribute the writer operation. No production inspection performed.

Source confirms two writer checkouts per SQLite probe (ping plus already-batched
schema inventory), a two-second runtime deadline, 15-second ticks, global
pre-handler 503 gating, and no helper retry for that response. This explains
propagation and delayed recovery, not the precise historical producer.
`ReceiveAgentDeliveryEvent` commits before returning success; receipt and
projection atomicity must survive any later optimization. Retention work is
bounded and maintenance-coordinated, but immediate continuations need measuring.

The implementation turn installed checksum-verified official Go 1.26.0 and
rootless Debian GCC packages. The deterministic catalog test reproduced the
second writer-checkout timeout and passed after moving that read to the WAL
reader. The 1/4/8-writer matrix passed with retention on and off, but did not
reproduce a two-second outage on the fast test filesystem. The historical
writer producer remains unattributed. See [evidence.md](evidence.md).

## Scope

In scope: local synthetic writer attribution, exact persistence-503 acquisition
retry, authorization/fault/integrity controls, fork review PR, deployment and
rollback instructions. Preserve all unrelated changes and task data.

Out of scope: production load, other agents, GitHub reconnection, permission
expansion, blind mutation retries, more writers, relaxed FULL durability,
disabled cleanup, timeout-only health fixes, merge or deployment.

## Technical approach

Follow the [credential design](../../specs/platform/system-design/git-credential-lease-reissue.md).
The demonstrated catalog checkout uses the existing WAL reader after the
mandatory writer ping. No retention or delivery transaction change is selected. If a
specific writer producer is demonstrated, record its exact method, occupancy,
query plan where relevant, and smallest safe correction before changing it.
The helper retry is a bounded recovery mechanism, not a claim that starvation
or terminal-settlement latency is solved.

## Tests

Task 01 records concurrency 1/4/8, producer-on/off controls, checkout wait deltas,
transaction durations, probe stages and healthy recovery. Task 02 covers
requirement 002.1-.5 and preserves 001.1-.8 and persistence 007.1-.8.
Use local HTTP/credential protocol integration instead of an artificial browser
flow. No external push or PR mutation is part of the load fixture.

## Work orders

- [x] [Task 01: Attribute contention locally](task-01-attribute-contention.md)
- [ ] [Task 02: Recover credential acquisition](task-02-recover-acquisition.md)

## Deployment and rollback acceptance

After user review/merge, build and pin a fork image digest containing the patch.
Record the prior image digest and take the normal supported backup. Verify the
actual helper binary revision used by local and remote runtimes; existing agents
may retain the old agentctl, so updating only the backend is insufficient.
Do not terminate unrelated sessions to refresh helpers.

Before production rollout, run an isolated deployment with synthetic sessions
at 1/4/8 concurrent delivery writers and a retained-event population near 2663.
Record successful acquisition, bounded exhaustion, readiness transitions,
writer waits, receipt/cursor/effect integrity, and absence of secrets in logs.
Include sustained writer denial, missing table, corruption and revoked scope.
A recoverable 13/30/44-second gate interval must recover within the 60-second
budget; sustained unavailability must remain blocked. Compare identical
before/after datasets and preserve raw sanitized results.

After rollout, observe normal traffic only; do not load-test tm-lab-kandev.
Rollback by restoring the prior image/helper versions, without deleting data,
restoring stale databases, changing grants or disabling cleanup. No schema change
is planned. Roll back for integrity regressions, secret disclosure, scope changes,
unbounded acquisition or worse writer occupancy. State explicitly that helper resilience and reduced probe contention do not
guarantee terminal-settlement progress under arbitrary sustained overload.

## Verification results

Implementation and focused checks passed; see [evidence.md](evidence.md).
Publication remains pending the restored runtime helper configuration.
The operator independently repaired storage latency; this patch is a bounded
code mitigation. No merge, deployment or completion signal has occurred.

## Risks

- Pool counters do not identify the producer; retention correlation is insufficient.
- A 60-second acquisition can exceed an enclosing command's shorter timeout.
- Real write unavailability can coexist with successful read-only schema probes;
  success of a probe alone is not proof of write capability.
- Retry bursts must remain bounded and jittered; no broad transport retry.
