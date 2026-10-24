---
id: "03-windows-evidence"
title: "Windows evidence and operator documentation"
status: in_progress
wave: 3
depends_on:
  - 02-native-lifecycle
plan: plan.md
requirements:
  - REQ-EXECUTORS-SSH-EXECUTOR-002
acceptance_criteria:
  - AC-EXECUTORS-SSH-EXECUTOR-002.1
  - AC-EXECUTORS-SSH-EXECUTOR-002.2
  - AC-EXECUTORS-SSH-EXECUTOR-002.3
  - AC-EXECUTORS-SSH-EXECUTOR-002.4
  - AC-EXECUTORS-SSH-EXECUTOR-002.5
  - AC-EXECUTORS-SSH-EXECUTOR-002.6
  - AC-EXECUTORS-SSH-EXECUTOR-002.7
system_design:
  - ../../specs/executors/system-design/ssh-executor-windows.md
---

# Task 03: Windows evidence and operator documentation

## Summary

Add the real Windows integration fixture and establish end-to-end evidence from
a Linux backend, directly and through a pinned gateway. Document the supported
platform only after that evidence exists.

## In scope

- Complete native Codex task and terminal/file operations on real Windows.
- Verify channel closure, reconnect, backend restart, stop isolation and ACLs.
- Publish accurate prerequisites and limitations; record skipped/failed tests honestly.

## Out of scope

Production deployment, gateway provisioning, Windows account creation, and
changes outside this work order's boundary.

## Acceptance

- Complete native Codex task and terminal/file operations on real Windows.
- Verify channel closure, reconnect, backend restart, stop isolation and ACLs.
- Publish accurate prerequisites and limitations; record skipped/failed tests honestly.

## Verification

Run from repository root using Bash. Tests named here must be added where absent;
a successful command matching no tests is not acceptance evidence. Use the
fixture configuration defined by the integration suite for real Windows checks;
never commit its addresses, identities, or credentials.

```bash
(cd apps/backend && go test -trimpath ./internal/agent/runtime/lifecycle -run '^TestSSHWindowsE2E$' -count=1 -v)
node --test scripts/validate-public-docs.test.mjs
node scripts/validate-public-docs.mjs
python scripts/list-docs.py validate
python scripts/lint-spec-files.py --all
git diff --check
```

Also run `bash scripts/lint-go-changed` with the exact changed Go paths.

## Files likely touched

- `apps/backend/internal/agent/runtime/lifecycle/executor_ssh_windows_integration_test.go`
- `docs/public/executors.md`
- `docs/public/windows-support.md`
- `docs/specs/executors/requirements/ssh-executor.md`
- `docs/specs/executors/system-design/ssh-executor-windows.md`

## Dependencies

02-native-lifecycle.

## Risks

See the paired design's Windows boundary and the plan's risks. Preserve Unix
behavior and fail closed when identity or private storage cannot be verified.

## Parallelism

`sequential`

## Inputs

- [SSH requirements](../../specs/executors/requirements/ssh-executor.md), proposed extension.
- [SSH design](../../specs/executors/system-design/ssh-executor-windows.md), proposed extension.
- Existing SSH fake-server and lifecycle tests; native Windows agentctl implementation.

## Results

Native integration evidence is recorded in task 02. Public prerequisites and
Windows script limitations are documented. Provider-authenticated Codex
inference remains unverified until the target account is authenticated.
Production rollout is being completed at the user's explicit request.
