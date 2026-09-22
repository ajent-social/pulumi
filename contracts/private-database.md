# infrastructure.private-database

Status: candidate; no implementation.

Intent: provision a database with explicit network, encryption, credential, backup and deletion behavior. Inputs include a private network reference, authorized client identities, engine/version, secret references and explicit retention/recovery choices. Outputs expose connection metadata and secret references, never plaintext credentials.

Proposed guarantees: no public ingress by default; encryption configured; least-privilege access; explicit backup/deletion choices. Availability, cost and restore objectives belong to the caller. Never silently choose destructive replacement behavior.

Tests must deny public exposure and unsafe configuration, inspect generated resources, and verify a disposable deployment's actual connectivity. Backup existence is not restore evidence: record a restore drill before claiming recoverability. No mock, policy, deployment or restore verification has run yet.
