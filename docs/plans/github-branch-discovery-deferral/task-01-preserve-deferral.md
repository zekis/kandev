---
id: "01-preserve-deferral"
title: "Preserve deferred branch discovery"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-INTEGRATIONS-GITHUB-PR-DISCOVERY-001
  - REQ-INTEGRATIONS-GITHUB-PR-DISCOVERY-HEALTH-001
  - REQ-INTEGRATIONS-GITHUB-PR-POLLING-002
acceptance_criteria:
  - AC-INTEGRATIONS-GITHUB-PR-DISCOVERY-001.1
  - AC-INTEGRATIONS-GITHUB-PR-DISCOVERY-001.2
  - AC-INTEGRATIONS-GITHUB-PR-DISCOVERY-001.3
  - AC-INTEGRATIONS-GITHUB-PR-DISCOVERY-HEALTH-001.2
  - AC-INTEGRATIONS-GITHUB-PR-DISCOVERY-HEALTH-001.3
  - AC-INTEGRATIONS-GITHUB-PR-DISCOVERY-HEALTH-001.4
  - AC-INTEGRATIONS-GITHUB-PR-DISCOVERY-HEALTH-001.5
  - AC-INTEGRATIONS-GITHUB-PR-POLLING-002.4
system_design:
  - ../../specs/integrations/system-design/github-pr-discovery-health.md
---

# Task 01: Preserve deferred branch discovery

## Summary

Keep local admission deferrals distinct from rejected provider requests through
searching-watch synchronization. Finish the original discovery under normal
admission before clearing that target's health failure.

## Owned files and dependencies

No implementation dependency on sibling tasks. Likely files:

- `apps/backend/internal/github/service_pr_watch_batched.go`
- `apps/backend/internal/github/service_pr_discovery_attempt.go`
- `apps/backend/internal/github/service_pr_watch.go`
- `apps/backend/internal/github/service_pr_watch_lookup.go`
- `apps/backend/internal/github/poller.go`
- `apps/backend/internal/github/service_pr_watch_deferral_test.go`
- `apps/backend/internal/github/service_pr_watch_deferral_health_test.go`
- `apps/backend/internal/github/service_pr_watch_deferral_scope_test.go`
- `apps/backend/internal/github/service_pr_watch_deferral_mixed_test.go`
- `docs/public/integrations.md` and the linked design/plan records.
- Existing batched budget tests remain unchanged and are included in validation.

Inspect existing scoped batch-progress helpers before adding state. Avoid
GraphQL decoder or credential resolver changes unless a regression proves one
necessary. No schema, public status DTO, frontend, rate policy, or grant changes.

## Implementation acceptance

1. Typed local deferral/cancellation survives apply and shared-attempt paths;
   it causes neither a new Unavailable failure nor false success, and unfinished
   watches remain retry-eligible without immediate per-watch fallback.
2. Retained stages eventually complete under real background pacing; a complete
   empty result clears only the matching failure. Completed mixed-batch siblings
   retain persistence and event publication. Old credential/target progress
   cannot apply to a replacement scope.
3. Tests fail before the correction and pass afterward, covering empty/new,
   merged/closed branch, linked PR, fork parent, mixed partial error, credential
   replacement, missing repository, denial, and provider-unavailable outcomes.

## TDD reproduction and regression matrix

Recreate `TestReproEmptyBranchAdmission` as a permanent focused test:
use `setupBatchedPollerTest`, `seedSearchingWatches`, a recording HTTP transport,
`branchAliasResponse`, and a real `RateCoordinator.coordinate` admission.
Attach it to the real TokenClient with `WithRateTracker`/`withRateAdmission`.
Use `WithNonBlockingGitHubAdmission(WithGitHubWorkClass(ctx,
WorkClassBackground))`; hold Core admission while GraphQL is available, then
call `syncWatchesBatchedWithClient` with generation 3. Assert four resolved
empty aliases produce no new failure, no successful check timestamp, no Core
HTTP request, and no completed-discovery claim. Also release a Core request
and test its normal pacing deferral with controlled time rather than sleeps.
Never print request headers or credentials in assertions.

Release admission and resume through the real poll/service entry point. Assert
forward progress and final healthy status, with exact request counts. Repeat
with a fork requiring two Core stages; ensure pacing cannot starve parent PR
lookup. Seed a real previous target failure and prove deferral preserves it,
while later completed empty discovery clears it.

Use identical empty OPEN responses for unpublished branches and branches with
only merged/closed PRs. Include a linked PR in the same batch and assert its
status/event survives. Test partial missing-repo aliases and real HTTP 403,
404, and 503 outcomes without treating them as successful negatives. Preserve
existing authentication/negative-cache semantics. Credential replacement uses
the resolver fixture and generation-specific scopes; complete an old in-flight
attempt after replacement and verify isolation. Retain current budget tests.

## Verification

Run sequentially from the repository root (VM 4 CPU/8 GB; sibling lint active):

```sh
GOMAXPROCS=2 CC=/tmp/kandev-cc/cc /tmp/kandev-toolchain/go/bin/go -C apps/backend test -trimpath -p 2 ./internal/github -run 'Test.*(Deferral|Deferred|SyncWatchesBatched|PRDiscoveryHealth|Batched|Fork)' -count=1
GOMAXPROCS=2 CC=/tmp/kandev-cc/cc /tmp/kandev-toolchain/go/bin/go -C apps/backend test -trimpath -p 2 ./internal/github -count=1
python3 scripts/list-docs.py validate
python3 scripts/lint-spec-files.py --all
git diff --check
```

Run `bash scripts/lint-go-changed` with the exact changed Go paths, setting
`GOMAXPROCS=2`, `GOFLAGS=-p=2`, `CC=/tmp/kandev-cc/cc`, and PATH containing
`/tmp/kandev-toolchain/go/bin` and
`/tmp/kandev-toolchain/golangci-lint-2.9.0-linux-amd64`.
Run documentation-coverage preflight for the final changed-file list before PR
publication. Do not overlap builds or start broad backend verification.

## Results

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
