# delivery.container-deploy

Status: CANDIDATE AWS ECS Fargate implementation. Package:
`github.com/ajent-social/pulumi/aws/containerdeploy`.

## Intent

Run one service from an **immutable image digest** (`repo@sha256:…`) on
Fargate in private subnets, with a required container port and CPU/memory.
Mutable tags (`:latest`) are rejected.

## Non-goals

Building/scanning images (see delivery.container-artifact), multi-tenant
schedulers, or choosing public IP by default (assignPublicIp must be explicit).
