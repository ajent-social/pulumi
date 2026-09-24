# infrastructure.tenant-dns-tls

Status: CANDIDATE AWS implementation. Package:
`github.com/ajent-social/pulumi/aws/tenantdnstls`.

## Intent

Issue an ACM certificate for `*.baseDomain` (DNS validated) and optionally
create a Route53 alias record for `slug.baseDomain` → load balancer DNS.
Base domain and hosted zone are caller-owned. Never trust request Host for
certificate SANs.

## Non-goals

Global CDN, multi-region failover, or registering the apex domain.

Apex + `*.baseDomain` share one ACM DNS validation CNAME under current AWS
behavior; this component creates that record at registration time. Distinct
validation CNAMEs for unrelated SANs are out of scope for this candidate.
