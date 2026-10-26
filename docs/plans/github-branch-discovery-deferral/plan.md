---
created: 2026-10-26
status: implemented
requirements:
  - REQ-INTEGRATIONS-GITHUB-PR-DISCOVERY-001
  - REQ-INTEGRATIONS-GITHUB-PR-DISCOVERY-HEALTH-001
  - REQ-INTEGRATIONS-GITHUB-PR-POLLING-002
system_design:
  - ../../specs/integrations/system-design/github-pr-discovery-health.md
legacy_specs: []
---

# Implementation Plan: GitHub branch discovery admission deferrals

## Overview

Repair searching-watch follow-up handling so valid empty branch responses do
not become Unavailable when a local admission slot is temporarily unavailable.
One sequential work order covers service outcomes, retry eligibility, and
regression tests. Integrations owns provider discovery and health; no UI change
or new requirement is necessary.

## Scope

Include typed follow-up deferrals, successful empty recovery, mixed batches,
credential replacement, scoped continuation, and genuine failure preservation.
Exclude parent contention PR1, runtime restoration task
46d24cdc-aff2-4e36-8c00-2966e8e1b61e, deployment, restarts, live database or
credential writes, grants, token changes, and Greatland work.

## Evidence and root cause

Base and remote main: `28301f9721b07c2b18948f069dea200357d0a7d4`.
Clean existing task branch: `feature/fix-false-github-bra-9qo`.
Authenticated `gh pr list` found no PR for this branch.

Read-only configured-helper probes on 2026-10-26 succeeded:

- The full `prFieldsBlock()` branch query for all four user-supplied branches
  returned four empty node lists with no errors (GraphQL cost 1).
- `GET /repos/zekis/open-connector` returned a non-fork repository.
- `GET /repos/zekis/kandev` identified parent `kdlbs/kandev`.
- `GET /repos/kdlbs/kandev/pulls?state=open&head=zekis%3Afeature%2Frestore-git-credenti-yym&per_page=100`
  returned an empty list.

A temporary `TestReproEmptyBranchAdmission` exercised the real TokenClient,
RateCoordinator, batched service, health store, and disposable SQLite test
store. Holding the Core background admission slot while GraphQL returned four
empty branch aliases produced this sanitized error:

```text
inspect repository zekis/open-connector for parent PR lookup:
get repository zekis/open-connector:
background provider admission deferred: background_in_flight
```

Only the GraphQL request reached the fixture server. Nevertheless, all four
results had `SyncFailed=true`, and generation-3 health became degraded with
four unavailable targets. The assertion that local deferral must not degrade
health failed on the unchanged implementation. A second run released the Core
slot before discovery and reproduced the same four failures from ordinary
`background_pacing`, with only one GraphQL request reaching the fixture.

`ResolvedEmpty` works: `lookupSearchingWatchPR` correctly skips a redundant
branch query and enters `findPRInForkParent`. `applyBatchedSearchingWatch`
discards the typed error, stamps the watch, and returns generic SyncFailed.
`syncWatchesBatchedWithClient` then records Unavailable. Thus fresh credential
scopes with cold fork metadata caches can hit the defect despite valid auth.

The diagnostic bundle contained no debug-level fallback messages. This proves
a code defect consistent with the incident, not the exact production admission
reason. No production state was changed. Credentials were never persisted.

## Technical approach

Keep the typed outcome through searching apply and shared attempts. Prioritize
branch follow-up reads before optional Actions enrichment and order them by
oldest check time. The existing credential-scoped fork metadata cache retains
the completed Core stage; unchanged timestamps prioritize unfinished branches.
No new cross-poll continuation or persistence is required. Defer only
unfinished targets, preserve successful siblings, and retain progress needed
to avoid starvation when sequential Core reads are paced. Do not clear health
on auth refresh or on a partial successful branch response. Complete the fork
network search before reporting success. Preserve negative-cache scoping,
credential generation fences, partial GraphQL errors, and exact-head identity.

| Path | Outcome | Evidence |
| --- | --- | --- |
| Token/PAT client, empty non-fork | Complete empty only after metadata succeeds | Real client + fake HTTP + real coordinator |
| Fork branch with no open parent PR | Complete empty after exact-head read | Pacing between metadata and parent read |
| Linked PR plus deferred branch | Publish linked success; retain deferred branch eligibility | Mixed batch service test |
| Credential replacement | New scope uses current client; stale continuation cannot apply | Generation replacement test |
| Missing/denied/provider error | Retain failure; never cache as successful empty | HTTP and partial GraphQL error tests |
| GH client | Same typed service outcome; retain command budget | Existing batched budget tests |

## Tests

Task 01 maps Discovery AC .1-.3, Health AC .2-.5, and Polling-002 AC .4 to
backend service and poller regression tests. Retain existing call-count tests.
Do not add a browser test for unchanged rendering; provider-to-health projection
and event publication supply end-to-end evidence at this backend boundary.

## Work orders

- [x] [Task 01: Preserve deferred branch discovery](task-01-preserve-deferral.md)

## Verification results

Passed on 2026-10-26, sequentially with `GOMAXPROCS=2`, `-p 2`,
`-trimpath`, and `CC=/tmp/kandev-cc/cc`:

- Focused GitHub regression/budget suite with
  `-run 'Test.*(Deferral|Deferred|SyncWatchesBatched|PRDiscoveryHealth|Batched|Fork)'`:
  pass (4.300s).
- Full `go test ./internal/github -count=1`: pass (135.399s).
- The same focused suite with `-race`: pass (6.091s).
- Final `-race -run '^TestSearchingWatchDeferral' -count=1`: pass (1.384s),
  including merged/closed branches and all four new test files after the final
  fixture cleanup. Production source was unchanged after the full-package run.
- `bash scripts/lint-go-changed` over all nine changed Go files: 0 issues.
  Initial lint identified two unused test-fixture assignments; both corrected.
- `python3 scripts/list-docs.py validate`: pass (403 decisions, 1729 specs).
- `python3 scripts/lint-spec-files.py --all`: pass.
- Repository `validateCoverage` preflight over actual changed paths: covered.
- `git diff --check`: pass.

TDD established failures before correction for busy/pacing misclassification,
fork follow-up progress, later-branch starvation, on-demand timestamp stamping,
and mixed-batch completion reporting. Tests retain actual token-client and
coordinator behavior with only HTTP replaced. Credential replacement and shared
attempt tests use explicit synchronization; no live credentials or data were
used. Public troubleshooting guidance was updated.

## Risks

Merely suppressing SyncFailed would hide starvation or clear genuine failures.
Stamping deferred work can postpone retries by the idle schedule. Replaying
completed metadata/GraphQL stages can repeatedly consume admission and prevent
parent search. A whole-batch early return can discard linked-PR events.
Continuation must remain bounded, credential-scoped, and invalidatable.

## Delivery

Implementation was explicitly authorized after the design handoff. Commit only
task changes. The normal task helper is absent after restart; the user withdrew
the authenticated-read prerequisite and authorized local checks and commit.
Publish through the normal helper only if its configuration becomes available;
otherwise the operator will publish the exact tested commit using existing
owner desktop credentials and create one PR to `zekis/kandev` main. No signing
authority, capability issuance, injected credentials, or grant changes are allowed.
Do not merge, enable auto-merge, or signal completion before the PR is ready.
