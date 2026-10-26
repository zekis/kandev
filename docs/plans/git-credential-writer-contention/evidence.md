# Contention and credential recovery evidence

## Scope and environment

Base: `28301f9721b07c2b18948f069dea200357d0a7d4`, fork `zekis/kandev`.
No deployed service, production database, or unrelated session was modified.
Go 1.26.0 came from the official go.dev archive, verified against its published
SHA-256. GCC 12 and headers were unpacked from Debian's configured signed
package repositories into `/tmp/kandev-cc`; no system install was required.
Go checks use `CGO_ENABLED=1`, that compiler, `GOMAXPROCS=2` or `4`, `-p 2` or `4`,
`-trimpath`, and `-tags fts5`.

The environment's disk-backed `/tmp` has appreciable fsync latency (an isolated
five-commit Python control took 0.269 seconds). The first large fixture run was
interrupted during schema initialization, not during delivery or a deadlock.
The full concurrency and repository-integrity matrix uses disposable tmpfs
files. SQLite still uses the real factory, WAL, immediate transactions and
synchronous FULL. This validates concurrency and data contracts, not physical
power-loss survival or deployed disk throughput. Disk-backed probe tests and
existing durability-configuration tests supply separate controls.

For tmpfs test runs only, `/tmp/kandev-test-exec` sets
`TMPDIR=/dev/shm/kandev-contention-tests` then execs its arguments. Build/link
scratch stays on disk. Setting TMPDIR for the compiler initially hit the 64MiB
shm capacity; no shared caches were deleted.

## Red evidence

- `TestHealthCatalogDoesNotReacquireBusyWriter` failed before the probe change:
  `table_probe`, `deadline_exceeded`, writer in use=1, writer waits delta=1,
  reader in use=0. After a successful writer ping, a real immediate transaction
  inserts an uncommitted row; the schema phase unnecessarily queues again.
  The corrected phase uses the reader without a writer wait. A full check while
  that transaction is held still fails at the mandatory writer ping.
- `TestGitHubCredentialPersistenceRecovery` failed at the first 503 for the
  13/30/44-second simulated outages. Exhaustion and cancellation regressions
  also failed under the original immediate-return behavior. They pass with the
  bounded retry. `testing/synctest` drives time without production test hooks.

## Workload and interpretation

`internal/task/repository/sqlite.TestCredentialContentionLoad` seeds 2663
historical events through real receipt/canonical-projection methods. Each
concurrent writer then receives and projects 50 events; an optional concurrent
retention loop performs 20 bounded batches. Checks retain one SQLite writer,
read pools, full schemas and real transaction boundaries. They verify final
received/projected cursors, replay duplicate suppression, integrity_check,
and receipt/effect counts after closing and reopening the database.

The initial green matrix showed queueing but zero probe failures at 1/4/8
writers, both with and without retention. This does not identify retention as
the production cause. The confirmed bottleneck removed is the schema phase's
unnecessary second writer checkout. The helper fix independently bridges a
bounded gate outage without weakening the server's decision or retrying a
mutation. No delivery transaction, receipt ACK, cleanup policy or durability
setting changed.

## End-to-end and fault controls

- Real controller/broker behind the real persistence middleware: credentials
  are returned before contention, withheld on gate 503, returned after recovery,
  and withheld after scope revocation or a missing table.
- Actual `git credential fill` launches the compiled helper entry point against
  a local transient-failure server; stdout contains only the protocol result
  and stderr is empty on recovery.
- The gh shim retries acquisition and invokes its underlying command exactly
  once, even when that command returns an ambiguous mutation failure.
- Existing lease scope/reissue tests remain in the focused suite. New checks
  cover one reissue, no reissue retry, the shared deadline including in-flight
  requests, no retry for unknown/malformed/provider/transport failures, bounded
  exhaustion without echoing server bodies, and actual corrupt SQLite files.

## Verification results

Saved final logs confirm these stable-source checks passed:

- Focused helper race suite: 3.270s; real Git protocol test and bounded recovery.
- Required-store probe race suite: 6.259s; database controls: 1.065s.
- All six 1/4/8-writer × retention-on/off race cases: 87.729s, zero probe failures;
  broker/middleware race fixture: 1.404s.
- Store conformance race suite: 131.267s; SQL safety guard passed.
- Full scoped credential package race suite rerun after restore: 1.042s.
- Changed-package Go lint rerun after restore: `0 issues.`
- Spec index/schema lint, spec-linter tests, public documentation tests and
  validation passed before restore; stable documentation checks rerun at delivery.

Commands use `go test -trimpath -tags fts5 -race` with the package/regex scopes
listed in the work orders. The workload uses the tmpfs execution wrapper above.
Scoped lint uses `bash scripts/lint-go-changed` for agentctl, requiredstores,
backendapp and task/repository/sqlite. No broad load experiment was repeated.
PostgreSQL service integration is unavailable without a test DSN; this change
uses the SQLite-only catalog path and does not modify PostgreSQL probing.

## Separate operational repair (operator-reported)

On October 26 the operator moved VM100 scsi0 live from vmdata-05
ZFS/Samsung 860 QVO to existing local-lvm on a separate SSD, retaining the
original as unused0. The same 12 × 4KiB fsync check improved from median
555.34ms / max 2544.25ms to median 6.955ms / max 7.5ms; readiness returned 200.
The operator also configured a persistent broker reissue signing key and
recreated the same image after a verified stopped-state backup. These are
operational changes, not effects of this patch. No durability setting changed.

The code reduces avoidable probe queueing and makes transient credential
acquisition recoverable. It does not fix slow storage or establish which
historical writer producer dominated the incident. Retention is not blamed
or redesigned. The seeded matrix exercises retention discovery alongside
live delivery; it is not a production pruning-throughput benchmark.

## Publication status

The restored command environment contains the broker signing-key configuration
(presence checked only), but no task credential scope, reissue capability or
Git credential-helper configuration. Standard `git credential fill` for
`https://github.com/zekis/kandev.git` currently fails with:
`fatal: could not read Username for 'https://github.com': No such device or address`.
The prior revoked/invalid lease errors predate the runtime restore.
No secret was printed/persisted, no signing key was used to mint authority,
and no alternate account, scope expansion or GitHub reconnect was attempted.
