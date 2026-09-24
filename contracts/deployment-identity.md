# infrastructure.deployment-identity

Status: candidate; AWS GitHub Actions OIDC profile implemented, not provider-verified or consumer-verified.

Intent: allow one trusted deployment workload to obtain short-lived cloud authority scoped to intended resources. Outputs identify the preview/apply principals and exact trust subject without exposing secrets.

## AWS GitHub Actions profile

The first profile constructs two IAM roles from the official Pulumi AWS provider resources: a preview role and an apply role. Both trust only the GitHub Actions OIDC provider ARN supplied by the caller, the exact audience `sts.amazonaws.com`, and distinct exact repository execution subjects. The caller must supply separate, valid IAM permission documents for both roles. Each `Allow` statement must name exact actions and exact resource ARNs. The only resource-wildcard exceptions are `ec2:DescribeSubnets` and `ecr:GetAuthorizationToken`, each constrained by exact `StringEquals` `aws:RequestedRegion` values. Wildcard actions, other wildcard resources, `NotAction`, and `NotResource` are rejected. Canonical-equivalent documents are rejected as a syntactic duplicate guard; IAM-equivalent documents with different presentation can pass. The caller remains responsible for the meaning of each role’s permissions.

A caller selects exactly one scope for preview (`Ref` or `Environment`) and one different scope for apply (`ApplyRef` or `ApplyEnvironment`):

- exact branch or tag ref, such as `refs/heads/release`; or
- exact GitHub environment name.

Owner and repository names are explicit. Optional immutable owner/repository IDs must be supplied together and form the immutable GitHub subject variant. No wildcard repository, ref, environment, audience, or provider default is generated. Environment protection rules, deployment branch restrictions, approval, AWS account setup, the existing OIDC provider, the Pulumi provider's own credentials, and organizational policy attachment remain caller-owned. An environment subject does not prove that environment approval is configured.

The component does not create an OIDC provider, permissions boundary, GitHub workflow, or AWS deployment. It does not classify arbitrary IAM actions as read-only; callers own the meaning of their preview and apply permission documents. Beyond the two documented, region-constrained exceptions, its strict resource rule rejects wildcard resources, including wildcard resource suffixes; deployments requiring dynamically named resources are unsupported until their permission requirements are reviewed.

A pure Go resource predicate and positive/negative fixtures cover raw IAM role trust and permission resources. Pulumi currently supports custom policy packs in TypeScript, Python, or OPA, not Go. The predicate is not automatically attached to a stack and establishes no organization-wide enforcement. Consumers must connect equivalent checks to their chosen policy engine and attach mandatory policy separately. Managed-policy attachment resources are rejected by the predicate because their effective policy cannot be verified from one resource at a time.

## Proposed guarantees and non-goals

No static cloud credential default; no wildcard subject or permission default; explicit separation of preview and apply roles; no implicit administrator policy. Provider-specific limitations remain visible rather than hidden behind a common abstraction.

Non-goals: organization-wide access management, universal cloud IAM, automatic deployment approval, creating GitHub environments or enforcing their protection settings.

## Evidence and limits

- Public provider and OIDC behavior is documented by the official [Pulumi AWS IAM Role resource](https://www.pulumi.com/registry/packages/aws/api-docs/iam/role/), [AWS GitHub OIDC guidance](https://docs.aws.amazon.com/IAM/latest/UserGuide/id_roles_create_for-idp_oidc.html), and [GitHub OIDC subject-claim reference](https://docs.github.com/en/actions/reference/security/oidc).
- Restricted maintainer-reported evidence describes an existing exact-repository/ref OIDC deployment role and an immediate requested second consumer. That evidence is not independently reproducible by public readers and does not establish adoption of this component.
- Component mock tests establish generated Pulumi resource properties. Pure policy fixtures establish only the tested validator behavior. No live AWS trust, denied assumption, policy attachment, policy-pack enforcement, or real consumer verification has been run.

Adoption gate: human review of the security API, provider-aware preview, disposable trust/denied-access verification with explicit cleanup, connected mandatory policy enforcement, and a real caller adoption. No production deployment is part of this slice.
