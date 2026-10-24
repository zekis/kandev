---
created: 2026-10-24
status: in_progress
requirements:
  - REQ-EXECUTORS-SSH-EXECUTOR-002
system_design:
  - ../../specs/executors/system-design/ssh-executor-windows.md
legacy_specs: []
---

# Implementation Plan: Native Windows SSH executor

## Overview

Run native Windows agents from Kandev through existing SSH transport. Deliver
platform discovery and verified artifacts, then the complete session lifecycle,
then real Windows evidence. This package extends the existing SSH requirement
and design rather than creating a parallel Windows executor contract.

## Assumption check

Confirmed: native Windows, existing SSH executor, an optional forwarding gateway,
and no WSL/Linux-worker substitution. Verified: native Windows agentctl support
exists, while remote discovery and lifecycle use Unix commands and release
helpers include only Linux/macOS. Initial implementation targets Windows amd64
and Windows PowerShell 5.1. Host SSH onboarding is a separate operational step;
no permission or endpoint is assumed ready from code completion.

## Scope

Discovery, helper packaging, native launch, SFTP paths, private credentials,
forwarding, recovery, session stop, orphan handling, and public documentation.
No new rendered UI layout: use existing connection results and errors.
No gateway changes, account creation, automatic SSH installation, deployment,
Windows arm64, or blanket changes to agent permissions.

## Technical approach

Use a remote-platform command/path boundary in lifecycle; preserve Unix paths.
The paired design identifies the current functions and packaging sources.
Thread platform information through all lifecycle consumers rather than using
the backend OS. Do not enable a partial Windows path while cleanup is still Unix.

| Target | Transport | Intended behavior | Evidence / fallback |
| --- | --- | --- | --- |
| Linux amd64/arm64 | Direct or one gateway | Existing native SSH | Existing lifecycle regressions |
| macOS amd64/arm64 | Direct or one gateway | Existing native SSH | Existing platform/recovery regressions |
| Windows amd64 | Direct or one gateway | Native PowerShell + agentctl | Real Windows smoke and contract tests |
| Other Windows architecture | Any | Reject before mutation | Unsupported-platform test |

## Tests

Planned files and test names (not yet implemented):

- `executor_ssh_windows_remote_test.go`: `TestSSHWindowsProbe`, `TestSSHWindowsPaths`,
  `TestSSHWindowsLifecycle`, `TestSSHWindowsRecovery`, `TestSSHWindowsOwnedStop`,
  `TestSSHWindowsPrivateCredentials`. Covers 002.1 through 002.6.
- Existing resolver/manifest tests plus `remote-helper-assets.test.mjs`:
  Windows checksum, identity, executable naming, missing/invalid artifact cases.
- Existing SSH lifecycle tests: 002.7, especially absence versus probe failure,
  restart recovery, credential merge, and cross-session stop isolation.

## E2E tests

Add an environment-gated Go integration suite, `TestSSHWindowsE2E`, in
`executor_ssh_windows_integration_test.go`. A Linux backend connects to a real
Windows OpenSSH host, directly and through a single pinned gateway. Test one
native Codex task, terminal/file operations, dropped SSH and backend restart,
controller channel closure, private ACLs, session stop and unrelated-process
preservation. Covers 002.1 through 002.6. The suite must skip explicitly without
private fixture configuration; skipped tests are not passing Windows evidence.

## Work orders

- [x] [01: Discovery and helper distribution](task-01-discovery-and-helpers.md)
- [x] [02: Complete native session lifecycle](task-02-native-lifecycle.md)
- [ ] [03: Windows evidence and operator documentation](task-03-windows-evidence.md)

Dependency order: 01 -> 02 -> 03. Execute sequentially.

## Verification results

Work order 01 is implemented and verified; see its Results for exact commands.
Work order 02 is implemented with real Windows startup, authenticated transport,
repository, Unicode path, file, terminal, recovery and stop evidence. See its
Results. Work order 03 retains provider-authenticated Codex inference as an
operator check. The user subsequently requested production deployment; the
feature is being validated for that rollout. No credentials or deployment
identifiers are included in this package.

The earlier design validation results below predate implementation.

- `python scripts/list-docs.py validate`: passed (403 decisions, 1729 specifications).
- `python scripts/lint-spec-files.py --all`: failed on 141 unrelated checkout
  findings; an invocation of the same linter filtered to the edited executor
  specifications found zero violations.
- Repository `validateCoverage` preflight with these work orders and the planned
  lifecycle runtime path: passed, all three work orders linked without errors.
  The runtime path was supplied as a preflight input; production code is unchanged.
- `git diff --check`: passed.
- `git status --short -- docs/plans/windows-ssh-executor`: confirms untracked
  plan/work orders. All artifacts remain unstaged and uncommitted.

## Risks

Windows OpenSSH child lifetime after channel closure, default cmd shell parsing,
Windows SFTP drive conventions, restrictive ACL verification, locked executable
replacement, and process identity reuse need executable evidence. Provider
availability differs on Windows; Codex is the initial acceptance target.
Production now follows the fork main image. Keep this implementation on its
feature branch until all required checks pass; pushing main automatically deploys.
