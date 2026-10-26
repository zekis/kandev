---
created: 2026-10-26
status: completed
requirements:
  - REQ-INTEGRATIONS-GITHUB-AUTHENTICATION-001
  - REQ-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-001
system_design:
  - ../../specs/integrations/system-design/github-authentication-02.md
---

# Managed Git credential restoration

Restore authorized Git access when lifecycle reconstructs a task workspace for terminals or UI Git operations after runtime loss. The workspace-only creation path rebuilt profile and repository environment definitions but omitted the normal managed broker issuance path. Independently, Git's noninteractive askpass setting named a nonexistent executable, and server signing authority could be inherited by child environments.

The user explicitly authorized implementation and focused tests without a design handoff. Work runs in the primary session on a dedicated branch based on `main`.

## Work order

1. [Restore scoped authorization and scrub server authority](task-01-restore-managed-git.md), completed.

## Boundaries

Keep the existing broker authorizer, credential generations, reissue capability validation, runtime admission, and persistence gates authoritative. Do not change GitHub connections, provider scope, repository identity, or the parent contention repair in PR #1. Do not deploy or merge. Same-user local execution remains a trusted-host boundary.
