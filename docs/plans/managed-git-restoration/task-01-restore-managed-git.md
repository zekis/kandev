---
id: "01-restore-managed-git"
title: "Restore scoped managed Git environments"
status: completed
plan: "plan.md"
requirements:
  - REQ-INTEGRATIONS-GITHUB-AUTHENTICATION-001
  - REQ-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-001
acceptance_criteria:
  - AC-INTEGRATIONS-GITHUB-AUTHENTICATION-001.16
  - AC-INTEGRATIONS-GITHUB-AUTHENTICATION-001.17
  - AC-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-001.3
  - AC-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-001.8
system_design:
  - ../../specs/integrations/system-design/github-authentication-02.md
---

# Restore managed Git environments

## Outcome and scope

Lifecycle workspace reconstruction calls the orchestrator's normal broker environment preparation after resolving explicit profile and repository settings and before creating the runtime. Read current task/session/repository bindings without cloning or rewriting origins. Reject missing or foreign bindings and unavailable profiles; never treat old leases as authority. The resulting instance environment feeds task shells and UI Git commands.

Remove the server reissue signing key after environment composition and at runtime forwarding, agent process, and task-shell inheritance boundaries. Keep explicit permitted task credentials. Disable Git askpass with an empty value while retaining disabled terminal prompts.

## Acceptance

1. Fresh launch and reconstruction yield exact repository-scoped broker capabilities; a new broker accepts the reconstructed lease and can reissue its capability, while old leases, changed scope, revoked grants, and removed connections fail closed.
2. Fresh/restored child environments exclude a synthetic server signing key, including inherited and explicit override sources, while retaining explicit Git tokens and scoped capabilities.
3. Existing shell/UI Git handoff and runtime persistence/admission checks still pass. Publish only a review PR.

## Files and dependencies

Own `internal/orchestrator/executor/executor_restored_git_credentials.go`, lifecycle workspace environment preparation, backendapp wiring, shared launchwrapper environment filtering, agentctl runtime forwarding and child/shell environment composition, and the Git noninteractive policy. No dependency on parent PR #1.

## Verification

From `apps/backend`, with Go 1.26 and CGO available:

```sh
GOMAXPROCS=2 go test -p 2 -trimpath -race ./internal/common/launchwrapper ./internal/common/subproc ./internal/gitcredentials ./internal/agent/runtime/agentctl/launcher ./internal/agentctl/server/shell ./internal/agentctl/server/process ./internal/agent/runtime/lifecycle ./internal/orchestrator/executor -run 'Test.*(RestoredWorkspace|RestoreWorkspaceGit|FreshAndRestored|ServerSigningKey|BrokerSigningKey|ManagedGit|GitEnvironment|GitCredential|ConfigureAndStartAgentKeepsLaunch|PrepareExecutionCreateRequest|ConfigureGit|Resume.*Credential|Credential.*Resume|MissingGitCredentials)' -count=1
go build -p 2 -trimpath ./internal/backendapp
```

From the root, run `bash scripts/lint-go-changed` with the changed Go files, `python3 scripts/list-docs.py validate`, `python3 scripts/lint-spec-files.py --all`, and `git diff --check`.

## Results

- The fresh task's configured Git helper filled credentials successfully with output captured only in memory. `git ls-remote origin refs/heads/main` and authenticated PR lookup succeeded.
- Regression failures reproduced missing restoration authorization, unavailable-profile fallback, the invalid askpass executable, and signing-key inheritance using synthetic values only.
- Focused race checks passed for lifecycle, executor, agentctl launcher/process, and signing-key boundaries. Full broker, launchwrapper, noninteractive Git, and shell suites passed with `-race`.
- The final environment-composition edit passed `go test -p 2 -trimpath -race ./internal/agent/runtime/lifecycle -run 'Test.*(BuildEnv|Environment|ManagedGit|RestoredWorkspace|PrepareExecutionCreateRequest|ConfigureAndStart)' -count=1`. The four affected signing-key test packages also passed their final race rerun.
- Production backendapp compilation, specification/catalog validation, public documentation validation/tests, and whitespace checks passed.
- The standard lint helper twice exhausted its five-minute package-loading timeout. The same changed-package scope completed with a fifteen-minute timeout and unchanged memory/concurrency bounds, reporting one complexity finding. After separating legacy composition from the final scrub, the standard helper passed on all remediated packages with zero issues.
- Git hooks and `pre-commit` are absent in this runtime; checks ran directly, without bypass flags. Publication uses the configured Git helper; review and deployment remain with the user.
- Production deployment and same-image recreation smoke validation are excluded from this authorized review-PR task.
