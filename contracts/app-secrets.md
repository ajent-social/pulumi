# infrastructure.app-secrets

Status: CANDIDATE AWS Secrets Manager implementation. Package:
`github.com/ajent-social/pulumi/aws/appsecrets`.

## Intent

Create named Secrets Manager secret **shells** (metadata + ARN only). No
plaintext secret values are accepted or written by this component. Callers
populate versions out of band (CLI, CI, or KMS-sealed pipelines).

## Non-goals

SSM Parameter Store, automatic rotation Lambdas, or generating passwords.

## Security limits

Passing plaintext into `Args` is a validation error. Outputs are ARNs and
names only. Recovery window and KMS key are caller choices with fail-closed
defaults (recovery window ≥ 7 unless `ForceDeleteWithoutRecovery`).
