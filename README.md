# AMSL infrastructure

Shared infrastructure should preserve security decisions, not just reduce resource declarations.

**Status: candidate AWS components.** This repository owns provider-specific Pulumi implementations and enforcement tests when their contracts are proven.

| Component | Contract |
| --- | --- |
| [aws/network](aws/network) | [VPC network](contracts/vpc-network.md) |
| [aws/httpsedge](aws/httpsedge) | [HTTPS edge](contracts/https-edge.md) |
| [aws/ecrrepo](aws/ecrrepo) | [ECR repository](contracts/ecr-repository.md) |
| [aws/oidcprovider](aws/oidcprovider) | [GitHub OIDC provider](contracts/github-oidc-provider.md) |
| [aws/appsecrets](aws/appsecrets) | [app secrets](contracts/app-secrets.md) |
| [aws/deploymentidentity](aws/deploymentidentity) | [deployment identity](contracts/deployment-identity.md) |
| [aws/privatedatabase](aws/privatedatabase) | [private databases](contracts/private-database.md) |
| [aws/ecscluster](aws/ecscluster) | [ECS cluster](contracts/ecs-cluster.md) |
| [aws/containerdeploy](aws/containerdeploy) | [container deploy](contracts/container-deploy.md) |
| [aws/tenantdnstls](aws/tenantdnstls) | [tenant DNS + TLS](contracts/tenant-dns-tls.md) |

<<<<<<< HEAD
## Compose order (standards AWS app)

Typical wiring without a product-specific “god” stack:

1. `oidcprovider` (once per account) → `deploymentidentity`
2. `network` (EnableNAT for private Fargate pulls)
3. `ecrrepo` → push digest-pinned images
4. `appsecrets` shells → populate out of band
5. `privatedatabase` on private subnets + data SG
6. `tenantdnstls` for `*.base` ACM → `httpsedge` with cert + public subnets + edge SG
7. `containerdeploy` on private subnets + app SG, register to edge target group (caller)
=======
`containerdeploy` accepts optional `TargetGroupARN` to attach to an HTTPS edge
target group. Prefer composing with the AWS substrate packages on
`feat/aws-standards-wave1` when merged (`network`, `httpsedge`, `ecrrepo`, …).
>>>>>>> d235718 (Add ECS cluster substrate and ALB target-group attach.)

Components encode construction defaults. An attached resource-level policy can catch unsafe raw resources and overrides. Mocks, policy tests and live disposable verification establish different facts; none should be described as another. No production deployment is part of bootstrap.

See the [verification matrix](docs/verification.md) and [canonical RFC](https://github.com/ajent-social/capabilities/blob/main/docs/rfc/0001-amsl-bootstrap.md). Provider selection remains an explicit implementation gate. There is no synthetic stack or placeholder cloud deployment to run.
