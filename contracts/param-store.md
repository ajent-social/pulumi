# infrastructure.param-store

Status: CANDIDATE AWS Systems Manager Parameter Store implementation. Package:
`github.com/ajent-social/pulumi/aws/paramstore`.

## Intent

Create named SSM parameter **shells** under `/Prefix/Name` with a non-secret
placeholder value and `Overwrite=false`. No caller plaintext is accepted in
`Args`. Populate real values out of band (CLI/CI). Use Secrets Manager
(`appsecrets`) when the secret must never appear as an SSM `String` value in
state after populate.

## Non-goals

Secrets Manager (see `appsecrets`), automatic rotation, or accepting Values in
Pulumi Args.

## Security limits

`SecureString` requires `KMSKeyID`. Outputs are ARNs and names only. The
placeholder is tagged `ajent-populate-oob=true`. Overwrite stays false so an
out-of-band value is not replaced by the placeholder on re-apply.
