---
status: active
system: platform
created: 2026-08-20
owners:
  - kandev
---
# Git Credential Lease Reissue Requirements

## Overview

A long-running execution can outlive the in-memory Git credential lease held by the backend. A workspace credential rotation or backend restart then leaves Git and the `gh` shim permanently unable to redeem credentials even though the task, session, repository, and current workspace connection remain valid.

## Requirements

### REQ-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-001: Git Credential Lease Reissue

**Intent:** A long-running execution can outlive the in-memory Git credential lease held by the backend. A workspace credential rotation or backend restart then leaves Git and the `gh` shim permanently unable to redeem credentials even though the task, session, repository, and current workspace connection remain valid.

#### Acceptance criteria

- **AC-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-001.1:** A managed Git credential helper SHALL make at most one reissue attempt for a single credential request after the broker reports a reissuable lease failure.
- **AC-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-001.2:** A reissue uses a dedicated execution capability, never the failed lease as proof of authority.
- **AC-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-001.3:** The broker SHALL issue the replacement lease only after it verifies the exact task, session, repository, provider, host, path, and optional provider-parent identity carried by the capability against live state.
- **AC-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-001.4:** A replacement lease SHALL bind to the current provider credential generation. A live execution therefore recovers after an authorized workspace credential rotation or after a backend restart discarded the old in-memory lease map.
- **AC-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-001.5:** Lease reissue SHALL occur only after lease-invalid, lease-expired, or lease-revoked responses. Lease-revoked is included because a credential-generation change deliberately revokes the old lease. Scope denial, malformed requests, capability failures, provider failures, and transport failures remain fail-closed with no retry.
- **AC-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-001.6:** Capabilities and leases are opaque bearer values. They SHALL never be logged, returned by browser-facing APIs, or stored with plaintext Git credentials.
- **AC-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-001.7:** **GIVEN** a running execution with a valid reissue capability, **WHEN** the workspace credential generation rotates and the old lease is redeemed, **THEN** the helper obtains one lease bound to the new generation and the same Git request succeeds.
- **AC-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-001.8:** **GIVEN** a running execution with a valid reissue capability, **WHEN** the backend restarts and has no prior lease records, **THEN** the helper obtains one newly issued lease and the same Git request succeeds.

## Transient persistence recovery

The helper provides bounded recovery from explicit persistence-gate rejection.
It adds a bounded recovery case to the existing helper contract without changing
which failures authorize lease reissue.

### REQ-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-002: Bounded persistence recovery

#### Acceptance criteria

- **AC-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-002.1:** When credential acquisition receives HTTP 503 with `persistence_unavailable`, it shall retry the same scoped acquisition within a total 60-second budget and at most 32 attempts. If readiness and authorization recover with enough budget for another request, acquisition shall succeed without a new login.
- **AC-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-002.2:** When required persistence remains unavailable, the helper shall return a bounded failure and emit no credential. Cancellation shall interrupt both request and backoff waits. Missing tables, corruption, closed stores, and write unavailability shall never become successful credential acquisitions merely because retries are enabled.
- **AC-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-002.3:** Transient recovery shall not expand repository, task, session, provider, host, path, or credential grants; fall back to another identity; or retry Git pushes, PR creation, or other mutations. Every admitted redemption shall retain live authorization and credential-binding checks.
- **AC-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-002.4:** Scope denial, malformed requests, provider errors, unclassified 503s, and ambiguous transport failures shall not trigger transient recovery. Lease reissue remains limited to one attempt under requirement 001.
- **AC-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-002.5:** Retry and exhaustion diagnostics shall contain no credential, lease, capability, request body, or raw server response. Successful credentials shall be written only to the credential protocol output.

## Out of scope

Changing persistence readiness policy, database durability, credential grants,
Git mutation retry, and automatic deployment. Technical details previously
embedded here are preserved in the [system design](../system-design/git-credential-lease-reissue.md).
