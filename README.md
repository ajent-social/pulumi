# AMSL infrastructure

Shared infrastructure should preserve security decisions, not just reduce resource declarations.

**Status: candidate AWS GitHub Actions deployment identity component. It is not provider-verified or consumer-verified.** This repository owns provider-specific Pulumi implementations and enforcement tests when their contracts are proven. The initial boundaries are [deployment identity](contracts/deployment-identity.md) and [private databases](contracts/private-database.md). The AWS component is documented in [aws/deploymentidentity](aws/deploymentidentity/README.md).

Components encode construction defaults. An attached resource-level policy can catch unsafe raw resources and overrides; the AWS candidate currently provides a pure predicate and test fixtures, not a Pulumi policy pack or attached enforcement. Mocks, policy tests and live disposable verification establish different facts; none should be described as another. No production deployment is part of bootstrap.

See the [verification matrix](docs/verification.md) and [canonical RFC](https://github.com/ajent-social/capabilities/blob/main/docs/rfc/0001-amsl-bootstrap.md). Provider selection remains an explicit implementation gate. There is no synthetic stack or placeholder cloud deployment to run.
