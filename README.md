# AMSL infrastructure

Shared infrastructure should preserve security decisions, not just reduce resource declarations.

**Status: contract foundation. No deployable components or policy pack are shipped yet.** This repository owns provider-specific Pulumi implementations and enforcement tests when their contracts are proven. The initial boundaries are [deployment identity](contracts/deployment-identity.md) and [private databases](contracts/private-database.md).

Components encode construction defaults. Resource-level policy catches unsafe raw resources and overrides. Mock tests, policy tests and live disposable verification establish different facts; none should be described as another. No production deployment is part of bootstrap.

See the [verification matrix](docs/verification.md) and [canonical RFC](https://github.com/ajent-social/capabilities/blob/main/docs/rfc/0001-amsl-bootstrap.md). Provider selection remains an explicit implementation gate. There is no synthetic stack or placeholder cloud deployment to run.
