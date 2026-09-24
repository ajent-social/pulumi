# AWS container deploy

Status: CANDIDATE. Capability: `delivery.container-deploy`.

Runs one ECS Fargate service from an immutable ECR digest (`repo@sha256:…`).
Mutable tags are rejected. Public IP is off unless `AssignPublicIP` is explicit.
Package: `github.com/ajent-social/pulumi/aws/containerdeploy`.

Does not build or scan images (see `delivery.container-artifact`), create
clusters/ALBs, or choose cost tiers.

See [contract](../../contracts/container-deploy.md).
