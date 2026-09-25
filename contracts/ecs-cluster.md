# delivery.ecs-cluster

Status: CANDIDATE AWS ECS cluster substrate. Package:
`github.com/ajent-social/pulumi/aws/ecscluster`.

## Intent

Create an ECS cluster plus Fargate **execution** and **task** IAM roles.
Execution role is region-scoped for ECR pull and CloudWatch Logs. Task role
assumes `ecs-tasks.amazonaws.com` with no product permissions attached.

## Non-goals

Service definitions (see `containerdeploy`), permissions boundaries, or
Spot capacity providers.
