---
id: "02-native-lifecycle"
title: "Complete native session lifecycle"
status: done
wave: 2
depends_on:
  - 01-discovery-and-helpers
plan: plan.md
requirements:
  - REQ-EXECUTORS-SSH-EXECUTOR-002
acceptance_criteria:
  - AC-EXECUTORS-SSH-EXECUTOR-002.2
  - AC-EXECUTORS-SSH-EXECUTOR-002.3
  - AC-EXECUTORS-SSH-EXECUTOR-002.4
  - AC-EXECUTORS-SSH-EXECUTOR-002.5
  - AC-EXECUTORS-SSH-EXECUTOR-002.6
  - AC-EXECUTORS-SSH-EXECUTOR-002.7
system_design:
  - ../../specs/executors/system-design/ssh-executor-windows.md
---

# Task 02: Complete native session lifecycle

## Summary

Integrate the Windows command/path boundary across launch, scripts, credentials,
skills, recovery, stop, reclamation and orphan inventory. Preserve the existing
transport and fingerprint trust model.

## In scope

- Native controller survives closing its launch channel and serves via SSH.
- Private staging and stdin prevent credential exposure; ACL failure stops launch.
- Recovery and cleanup verify process ownership and preserve uncertain processes.

## Out of scope

Production deployment, gateway provisioning, Windows account creation, and
changes outside this work order's boundary.

## Acceptance

- Native controller survives closing its launch channel and serves via SSH.
- Private staging and stdin prevent credential exposure; ACL failure stops launch.
- Recovery and cleanup verify process ownership and preserve uncertain processes.

## Verification

Run from repository root using Bash. Tests named here must be added where absent;
a successful command matching no tests is not acceptance evidence. Use the
fixture configuration defined by the integration suite for real Windows checks;
never commit its addresses, identities, or credentials.

```bash
(cd apps/backend && go test -trimpath ./internal/agent/runtime/lifecycle -run 'TestSSH|Test.*SSH' -count=1)
(cd apps/backend && go test -trimpath ./internal/agent/runtime ./internal/ssh -count=1)
```

Also run `bash scripts/lint-go-changed` with the exact changed Go paths.

## Files likely touched

- `apps/backend/internal/agent/runtime/lifecycle/executor_ssh_operations.go`
- `apps/backend/internal/agent/runtime/lifecycle/executor_ssh.go`
- `apps/backend/internal/agent/runtime/lifecycle/executor_ssh_credentials.go`
- `apps/backend/internal/agent/runtime/lifecycle/executor_ssh_scripts.go`
- `apps/backend/internal/agent/runtime/lifecycle/executor_ssh_skills.go`
- `apps/backend/internal/agent/runtime/lifecycle/executor_ssh_recovery.go`
- `apps/backend/internal/agent/runtime/lifecycle/executor_ssh_reclaim.go`
- `apps/backend/internal/agent/runtime/lifecycle/executor_ssh_orphan_sweep.go`

## Dependencies

01-discovery-and-helpers.

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

Implementation completed. The native Windows OpenSSH fixture is now reachable
through a pinned SSH gateway. A separate SSH login confirmed the native Windows
shell, and disposable process-lifetime probes established:

- `Start-Process` with explicit output redirection did not survive closing the
  launching SSH channel; its delayed marker was absent on a separate connection.
- `Invoke-CimMethod Win32_Process.Create` returned success, and its delayed
  marker was present after the launching channel closed. This is a viable
  supervisor candidate, subject to authenticated controller, ACL, identity,
  environment, recovery, and stop verification before enabling Windows launch.
- Git, Node, and npx were absent from the SSH account's current command search
  path. Agent preparation still needs these prerequisites addressed.

Temporary probe files were removed using resolved paths inside the account's
temporary directory. No permanent service or machine configuration was changed.
No credentials or real deployment identifiers are stored in this package.


The implementation now covers PowerShell staging and environment import,
checksum-qualified helper installation, private ACLs, WMI launch, native Git
preparation, file access, ConPTY shells, process identity, restart recovery,
owned stop, conservative orphan inventory and task reclamation.
`TestSSHWindowsE2E` passed through a pinned gateway for detached startup,
authenticated handshake, reconnect and stop. `TestSSHWindowsFullCreateE2E`
passed normal CreateInstance, native repository preparation, file access,
terminal start/stop, backend-style recovery and confirmed process absence
after stop. Its workspace includes spaces and a non-ASCII character.
Existing SSH regressions (`TestSSH|Test.*SSH`) and shell/ptyexec tests passed.

The Windows host needed Git and Node installed before agent use. Codex CLI
and the ACP bridge were installed and the Codex CLI version check succeeded.
A provider-authenticated inference prompt remains an operator acceptance check;
no provider credential was copied or inference usage incurred by these tests.

Final transport validation exposed the Windows exec command-length boundary
when a hardened stop script grew beyond cmd.exe limits. Commands over the
bounded encoded-command threshold now stage a BOM-encoded PowerShell file in
a verified private directory, preserve caller data on stdin, and remove the
file after completion. The long-command regression and both real Windows
integration tests passed, including confirmed controller shutdown.
