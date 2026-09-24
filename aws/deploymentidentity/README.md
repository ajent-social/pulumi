# AWS GitHub Actions deployment identity

This AWS-specific Pulumi component creates separate preview and apply IAM roles that trust one exact GitHub Actions OIDC subject. It uses the official Pulumi AWS provider and does not create or look up the account's OIDC provider. The module requires Go 1.25.11 or later, matching Pulumi SDK v3.256.0; it pins the official AWS provider SDK v6.83.4 and Pulumi SDK v3.256.0.

## Example

```go
identity, err := deploymentidentity.NewGitHubActionsDeploymentIdentity(ctx, "ferro-release", deploymentidentity.Args{
    RoleNamePrefix:    "ferro-release",
    ProviderARN:       "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com",
    Audience:          deploymentidentity.GitHubOIDCAudience,
    RepositoryOwner:   "example-org",
    RepositoryName:    "example-repo",
    Ref:               "refs/heads/release",
    PreviewPolicyJSON: previewPolicyJSON,
    ApplyPolicyJSON:   applyPolicyJSON,
})
if err != nil {
    return err
}
ctx.Export("previewRoleArn", identity.PreviewRoleARN)
ctx.Export("applyRoleArn", identity.ApplyRoleARN)
```

Set exactly one of `Ref` or `Environment`. `Ref` is an exact `refs/heads/...` or `refs/tags/...` value. `Environment` is an exact environment name. Set both `OwnerID` and `RepositoryID` to use GitHub's immutable subject format. Audience is fixed to `sts.amazonaws.com` and must be supplied explicitly.

`PreviewPolicyJSON` and `ApplyPolicyJSON` are required IAM identity policies. Each Allow statement must use exact actions and literal resource ARNs without `*` or `?`. The documents must differ. The component does not infer read-only preview actions or grant permissions on the caller's behalf.

## Security limits

The component supplies role construction defaults, not an AWS deployment boundary. The provider ARN, AWS account setup, caller permissions, Pulumi provider credentials, permissions boundary, and environment approval/protection settings stay with the caller. No static AWS access keys are created.

The exported `ValidateResource` predicate is a pure Go check for IAM role trust and permission resources. It can catch tested raw-resource bypasses when consumers connect equivalent logic to their policy enforcement. It is not automatically attached to Pulumi stacks; policy enforcement and drift detection are separate and unverified. Pulumi's current custom policy pack runtimes do not include Go, so a consuming stack needs a supported policy-pack integration or equivalent policy engine.

The component mock tests verify generated properties only. No live AWS trust behavior, denied access, actual policy enforcement, or consumer adoption has been verified. Do not treat this candidate as approved for production use without human security review and the provider/consumer evidence in the [deployment identity contract](../../contracts/deployment-identity.md).

## Alternatives reviewed

- The official [Pulumi AWS IAM Role resource](https://www.pulumi.com/registry/packages/aws/api-docs/iam/role/) already manages role trust and policy properties; this component composes that provider resource rather than introducing a cloud API client.
- Pulumi's separate [AWS IAM component package](https://www.pulumi.com/registry/packages/aws-iam/) is marked deprecated, so this component does not add it as a dependency.
- AWS recommends exact `aud` and `sub` trust conditions for GitHub OIDC; see [AWS's guide](https://docs.aws.amazon.com/IAM/latest/UserGuide/id_roles_create_for-idp_oidc.html) and [GitHub's OIDC subject reference](https://docs.github.com/en/actions/reference/security/oidc). GitHub now supports immutable owner and repository IDs in default subjects, so both exact formats are supported.
