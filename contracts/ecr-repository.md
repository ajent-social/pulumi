# delivery.ecr-repository

Status: CANDIDATE AWS ECR implementation. Package:
`github.com/ajent-social/pulumi/aws/ecrrepo`.

## Intent

Create an ECR repository with **immutable tags** and **scan on push**. Force
delete is off unless the caller sets `ForceDelete` for disposable stacks.

## Non-goals

Image builds, signing, cross-account replication, or lifecycle policies that
delete digests still referenced by running tasks.

## Security limits

Mutable tags (`MUTABLE`) are rejected. The component does not prove scanner
findings were remediated before deploy.
