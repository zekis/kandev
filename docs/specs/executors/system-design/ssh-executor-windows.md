---
status: draft
system: executors
requirements:
  - REQ-EXECUTORS-SSH-EXECUTOR-002
created: 2026-10-24
owners:
  - kandev
---

# Native Windows SSH Executor System Design

## Proposed native Windows extension

This section is proposed, not a description of deployed behavior. It extends
REQ-EXECUTORS-SSH-EXECUTOR-002 within the existing SSH executor; it does not add
another executor type or transport. The existing Unix behavior remains unchanged until implementation.

### Extension mapping

| Criteria | Technical boundary |
| --- | --- |
| 002.1, 002.7 | Platform discovery and prerequisite probes |
| 002.2, 002.6 | Helper distribution, paths, scripts, and private staging |
| 002.3 | Existing authenticated SSH forwards and controller handshake |
| 002.4, 002.5 | Native controller lifetime, ownership, and recovery |

All criterion suffixes in this table refer to
`AC-EXECUTORS-SSH-EXECUTOR-002`.

### Platform discovery and command boundary

`detectRemoteInfo` in `executor_ssh_operations.go` currently requires `uname`.
Keep its Unix probe and add a bounded PowerShell probe for native Windows when
Unix discovery does not identify a supported platform. Do not interpret an SSH
transport error as permission to launch a different probe indefinitely. Query
OS architecture, user home, PowerShell version, and Git explicitly; distinguish
unsupported architecture, missing prerequisites, and unreadable information.

Introduce a target-platform command/path boundary in the lifecycle package.
The backend host's `runtime.GOOS` and `filepath` semantics must not select remote
commands. Unix retains the existing shell implementation. Windows uses explicit
`powershell.exe -NoLogo -NoProfile -NonInteractive -EncodedCommand` with UTF-16LE
encoding, strict errors, and deliberate native exit-code propagation. Encoding
is quoting, not secret protection. Secrets go over stdin or private SFTP files.
PowerShell 5.1 is the baseline; PowerShell 7 is not a new dependency.

Represent remote native paths separately from SFTP protocol paths. Resolve
Windows home from the authenticated account and verify SFTP's canonical drive
path before writes. Reject traversal and paths outside the configured root.
Use literal path operations and test spaces, apostrophes, Unicode, drive roots,
CRLF, and shell metacharacters. Do not treat a user-configured Unix shell as a
Windows command runner. Existing profile defaults remain compatible.

### Helper distribution and private staging

Extend `SSHRemotePlatform`, `requireSupportedRemotePlatform`,
`agentctl_resolver.go`, `remote_helper_manifest.go`, the backend Makefile, and
`scripts/release/remote-helper-assets.mjs` for `windows/amd64`. Inventory the
release workflow, runtime archives, and container helper-copy lists together;
adding a manifest entry alone is insufficient. Use the Windows executable
suffix consistently and retain manifest identity, checksum, size, and cache
validation. Windows arm64 remains rejected.

Use SFTP temporary uploads and verified promotion. Windows cannot reliably
replace a running executable; install immutable version/hash-qualified helpers
and retain versions referenced by live sessions. Failed uploads must not corrupt
a usable cached helper. Before uploading sensitive files, create and verify a
private Windows directory ACL for the authenticated SID plus SYSTEM and local
Administrators, with broad inherited access removed. POSIX chmod success is not
proof of a private Windows ACL. Abort before secret upload on ACL failure.
Preserve existing credential merge and source allowlist semantics.

### Controller lifetime and recovery

Audit launch, script execution, skills, credential paths, logs, PID/port files,
reclamation, orphan sweep, and restart recovery; a platform switch confined to
startup is insufficient. Use the existing native Windows agentctl process and
Job Object support for children. Launch through a Windows-native detached
supervisor with explicit standard handles; prove in a real OpenSSH session that
closing the launch channel does not terminate the controller. Keep credentials
out of process arguments and the supervisor environment diagnostics.

Persist and verify process identity using PID plus creation time, executable
path, session directory, and the existing authenticated controller handshake.
Do not kill a process based only on a PID file. Graceful stop uses the existing
control protocol where available; bounded Windows termination is allowed only
for verified owned processes. Never use machine-wide name-based taskkill.
Probe failures preserve processes and block replacement. Orphan inventory uses
structured Windows process metadata and the existing task/session eligibility
rules. If identity cannot be proven, preserve and report it.

Retain existing loopback binding, per-session forwards, host fingerprint
pinning, bastion verification, and transport cancellation. No OCGW-specific
secret or deployment address enters Kandev source. A gateway is an ordinary
SSH ProxyJump configuration.

### Validation and rollout boundary

Use fake SSH tests for deterministic commands and errors, release-asset tests
for distribution, and a real disposable Windows OpenSSH destination for channel
closure, ACLs, SFTP paths, reconnect, stop, and a Codex task. Run from a Linux
backend directly and through a pinned single-hop gateway. Capture only redacted
results outside Git. A Windows cross-compile alone does not establish support.
Update public executor/Windows documentation only when implementation and real
Windows evidence exist. No production deployment is part of the design turn.
