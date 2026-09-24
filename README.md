# AMSL infrastructure

Shared infrastructure should preserve security decisions, not just reduce resource declarations.

**Status: candidate AWS components.** This repository owns provider-specific Pulumi implementations and enforcement tests when their contracts are proven.

| Component | Contract |
| --- | --- |
| [aws/deploymentidentity](aws/deploymentidentity) | [deployment identity](contracts/deployment-identity.md) |
| [aws/privatedatabase](aws/privatedatabase) | [private databases](contracts/private-database.md) |
| [aws/containerdeploy](aws/containerdeploy) | [container deploy](contracts/container-deploy.md) |
| [aws/tenantdnstls](aws/tenantdnstls) | [tenant DNS + TLS](contracts/tenant-dns-tls.md) |

Components encode construction defaults. An attached resource-level policy can catch unsafe raw resources and overrides. Mocks, policy tests and live disposable verification establish different facts; none should be described as another. No production deployment is part of bootstrap.

See the [verification matrix](docs/verification.md) and [canonical RFC](https://github.com/ajent-social/capabilities/blob/main/docs/rfc/0001-amsl-bootstrap.md). Provider selection remains an explicit implementation gate. There is no synthetic stack or placeholder cloud deployment to run.
