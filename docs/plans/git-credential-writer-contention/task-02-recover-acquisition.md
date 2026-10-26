---
id: "02-recover-acquisition"
title: "Recover scoped credential acquisition after persistence rejection"
status: in_progress
wave: 2
depends_on:
  - "01-attribute-contention"
plan: "plan.md"
requirements:
  - REQ-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-001
  - REQ-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-002
  - REQ-PLATFORM-POSTGRES-DOMAIN-STORE-PARITY-007
acceptance_criteria:
  - AC-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-001.1
  - AC-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-001.3
  - AC-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-001.5
  - AC-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-001.6
  - AC-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-002.1
  - AC-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-002.2
  - AC-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-002.3
  - AC-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-002.4
  - AC-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-002.5
  - AC-PLATFORM-POSTGRES-DOMAIN-STORE-PARITY-007.3
  - AC-PLATFORM-POSTGRES-DOMAIN-STORE-PARITY-007.4
system_design:
  - ../../specs/platform/system-design/git-credential-lease-reissue.md
  - ../../specs/platform/system-design/postgres-domain-store-parity.md
---

# Task 02: Recover scoped credential acquisition

## Summary

Implement only the positively classified pre-handler persistence-503 recovery
specified by the credential design. Use TDD and preserve the existing single
lease-reissue flow. Do not put retries into the generic HTTP post method.

## In scope

- Use the existing WAL reader for the already-batched schema inventory after
  both pool pings. The failing `TestHealthCatalogDoesNotReacquireBusyWriter`
  demonstrates the unnecessary second writer checkout; preserve missing-table,
  corruption, closed-pool and writer-unavailable failures.
- Red regression: a local broker responds persistence-503 then a valid credential;
  current helper stops after one attempt. Assert exact request identity throughout.
- Bounded jitter, attempt cap, one total deadline, cancellation during wait/request,
  immediate success, 13/30/44-second recovery simulations, and permanent 503.
- Invalid/expired/revoked lease paths: at most one reissue, replacement resolve
  shares remaining budget, reissue failures never loop.
- No retry on malformed/unknown response, provider failure, transport ambiguity,
  forbidden scope or capability failure; no credentials on failure or in diagnostics.
- Integrate actual credential helper binary via `git credential fill` against the
  local fixture from Task 01. Verify one successful protocol response and unchanged
  scope. Exercise the managed gh shim with a stub command counting invocations;
  credential retry must not replay the underlying command.
- Run the fault/integrity matrix from Task 01 after the change. Record limitations,
  deployment/helper-version checks and rollback instructions in the PR.

## Out of scope

Changing server health policy or database settings, retrying reissue or Git/PR mutations,
adding credential sources/grants, automatic merge/deployment, and speculative
writer optimization. Reconcile this work order before any demonstrated producer
optimization adds production files.

## Acceptance

1. Transient rejection recovers within the shared bound; sustained real failures
   remain blocked. Git and gh consumers preserve exact authority and invoke their
   underlying mutation at most once.
2. Regression evidence covers red/green, cancellation, fault controls, secret-free
   retry/exhaustion output, existing reissue behavior and delivery integrity.
3. Focused checks pass, task-only commit is pushed to the existing task branch,
   and a review PR targets `zekis/kandev:main`; no merge or deployment. If producer
   attribution remains unresolved, report that limitation without claiming the
   broader starvation problem is solved. The operator has authorized delivery
   of this bounded mitigation following the independent storage repair.

## Verification

Run sequentially from the repository root after the named tests exist:

```bash
(cd apps/backend && go test -trimpath -tags fts5 ./cmd/agentctl -run 'Test(GitHubCredential|GitCredential)' -count=1)
(cd apps/backend && go test -trimpath -tags fts5 -race ./internal/gitcredentials ./internal/persistence/requiredstores -count=1)
(cd apps/backend && go test -trimpath -tags fts5 -race ./cmd/agentctl -run 'Test(GitHubCredential|GitCredential)' -count=1)
(cd apps/backend && go test -trimpath -tags fts5 ./internal/backendapp -run 'Test(CredentialContentionLoad|RequiredPersistenceMiddleware)' -count=1 -timeout=5m)
bash scripts/lint-go-changed apps/backend/cmd/agentctl/github_credential.go apps/backend/cmd/agentctl/github_credential_retry.go apps/backend/cmd/agentctl/github_credential_retry_test.go apps/backend/internal/backendapp/credential_contention_test.go
python3 scripts/list-docs.py validate
python3 scripts/lint-spec-files.py --all
git diff --check
```

If evidence justifies a persistence production change, add its exact tests,
`go run ./cmd/sqlguard ./internal`, and race store-conformance checks before
expanding implementation. Document unavailable PostgreSQL coverage explicitly.

## Files likely touched

- `apps/backend/cmd/agentctl/github_credential.go`
- `apps/backend/cmd/agentctl/github_credential_retry.go` (new)
- `apps/backend/cmd/agentctl/github_credential_retry_test.go` (new)
- `apps/backend/internal/backendapp/credential_contention_test.go` (Task 01)
- `apps/backend/internal/persistence/requiredstores/health.go` and contention/fault tests
- `apps/backend/internal/task/repository/sqlite/credential_contention_test.go` and shared benchmark setup
- `apps/backend/cmd/agentctl/github_credential_git_test.go`
- `docs/public/integrations.md`
- Requirements/design and this plan's evidence/results.

## Dependencies

Task 01. Read `cmd/agentctl` scoped instructions if present and apply `/tdd`.
Use `/docs-maintainer` to document user-visible helper behavior before delivery.
Publication is already authorized; use repository commit/push/PR skills and
configured Git helper credentials in memory. Do not ask for GitHub reconnection.

## Risks

A retry budget cannot guarantee arbitrary overload recovery. Existing helpers
in retained runtimes require version verification; backend replacement alone
may not update them. Fake-time tests do not replace real local integration.

## Parallelism

`sequential`

## Inputs

Credential requirement/design, Task 01 evidence, existing helper and reissue
tests, backend persistence middleware tests and scoped broker tests.

## Results

Implementation and focused validation complete; see [evidence.md](evidence.md).
PR publication is pending managed task-helper restoration. No merge or deployment.
