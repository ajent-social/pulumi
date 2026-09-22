# infrastructure.deployment-identity

Status: candidate; no provider implementation selected.

Intent: allow a trusted deployment workload to obtain short-lived cloud authority scoped to intended resources. Inputs must explicitly identify issuer/audience, repository or workload subject, environment/ref restrictions and resource policy. Outputs identify the principal and trust configuration without exposing secrets.

Proposed guarantees: no static cloud credential default; no wildcard subject default; explicit separation of preview and apply authority; no implicit broad administrator binding. Provider-specific capability gaps must be visible rather than hidden behind a common abstraction.

Non-goals: organization-wide access management, universal cloud IAM, automatic deployment approval.

Denied-path requirements: wrong repository, audience, subject, environment/ref and resource scope cannot assume or use the identity. Verify token claims and trust at the provider, not just generated JSON. An environment claim does not prove approval settings exist.

Adoption gate: one provider-specific component, generated-resource tests, policy denial fixtures and a disposable live trust test with explicit cost/cleanup scope. Then a real caller adopts the component. No live verification is recorded yet.
