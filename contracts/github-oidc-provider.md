# infrastructure.github-oidc-provider

Status: CANDIDATE AWS IAM OIDC provider implementation. Package:
`github.com/ajent-social/pulumi/aws/oidcprovider`.

## Intent

Create the account-level IAM OIDC identity provider for GitHub Actions
(`token.actions.githubusercontent.com`) with audience `sts.amazonaws.com`, so
`infrastructure.deployment-identity` can trust exact repository subjects.

## Non-goals

Creating deployment roles, permissions boundaries, or GitHub environments.
One provider per issuer URL per account — do not create duplicates.

## Security limits

AWS verifies GitHub’s TLS chain via its CA bundle; the API still requires a
well-formed thumbprint at create time. Roles and subjects remain caller-owned
via `deploymentidentity`. This component does not grant any IAM permissions.
