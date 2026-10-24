---
id: "01-discovery-and-helpers"
title: "Discovery and helper distribution"
status: done
wave: 1
depends_on: []
plan: plan.md
requirements:
  - REQ-EXECUTORS-SSH-EXECUTOR-002
acceptance_criteria:
  - AC-EXECUTORS-SSH-EXECUTOR-002.1
  - AC-EXECUTORS-SSH-EXECUTOR-002.2
  - AC-EXECUTORS-SSH-EXECUTOR-002.7
system_design:
  - ../../specs/executors/system-design/ssh-executor-windows.md
---

# Task 01: Discovery and helper distribution

## Summary

Add platform probing, explicit remote path semantics, and checksummed Windows
amd64 helper resolution/build/release support. Keep Windows launch unavailable
until the complete lifecycle is implemented.

## In scope

- Detect native Windows with structured prerequisites and bounded failures.
- Resolve and validate Windows helper assets without changing Unix behavior.
- Cover missing artifacts, unknown architecture, unsafe paths and quoting.

## Out of scope

Production deployment, gateway provisioning, Windows account creation, and
changes outside this work order's boundary.

## Acceptance

- Detect native Windows with structured prerequisites and bounded failures.
- Resolve and validate Windows helper assets without changing Unix behavior.
- Cover missing artifacts, unknown architecture, unsafe paths and quoting.

## Verification

Run from repository root using Bash. Tests named here must be added where absent;
a successful command matching no tests is not acceptance evidence. Use the
fixture configuration defined by the integration suite for real Windows checks;
never commit its addresses, identities, or credentials.

```bash
(cd apps/backend && go test -trimpath ./internal/agent/runtime/lifecycle -run 'TestSSHWindowsProbe|TestSSHWindowsPaths|Test.*RemotePlatform|Test.*RemoteHelper|Test.*ResolveRemote' -count=1)
node --test scripts/release/remote-helper-assets.test.mjs
```

Also run `bash scripts/lint-go-changed` with the exact changed Go paths.

## Files likely touched

- `apps/backend/internal/agent/runtime/lifecycle/executor_ssh_operations.go`
- `apps/backend/internal/agent/runtime/lifecycle/agentctl_resolver.go`
- `apps/backend/internal/agent/runtime/lifecycle/remote_helper_manifest.go`
- `apps/backend/Makefile`
- `scripts/release/remote-helper-assets.mjs`
- `.github/workflows/release.yml`

## Dependencies

None.

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

Completed the foundation; Windows task launch remains explicitly gated until
work order 02 completes. No deployment or main push was performed.

- RED: Windows host discovery, executable naming, PowerShell encoding, manifest
  acceptance, native CLI lookup, and remote-home path tests failed on the prior
  behavior before their respective fixes.
- Linux Go runner: `go test -p 2 -trimpath ./internal/agent/runtime/lifecycle
  -run 'TestSSHWindows|Test.*RemotePlatform|Test.*RemoteHelper|Test.*ResolveRemote|TestDetectRemoteInfo|TestExpandRemoteHome'
  -count=1` passed after the final edit.
- `node --test scripts/release/remote-helper-assets.test.mjs
  scripts/release/publish-npm.test.mjs`: 17 passed.
- `bash scripts/release/runtime-bundle.test.sh`: passed, including helper asset
  tests, exact build output, stale bundle replacement, and failed-copy protection.
- `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -p 2 -trimpath
  -o bin/agentctl-windows-amd64.exe ./cmd/agentctl`: passed.
- `bash scripts/lint-go-changed` over the changed lifecycle Go source/tests:
  passed, zero issues. The first cold run exhausted its five-minute budget;
  the final run passed using the populated cache.
- Action SHA-pinning check and `git diff --check`: passed.

Verification used a separate Linux source tree and bounded test containers.
Shell copies were normalized to LF and their executable modes restored after
transfer from the Windows worktree; these are fixture preparation steps.
Native Windows SSH session startup has not yet been tested.

