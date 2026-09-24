# AWS tenant DNS + TLS

Status: CANDIDATE. Capability: `infrastructure.tenant-dns-tls`.

Issues a DNS-validated ACM certificate for `baseDomain` + `*.baseDomain` and
optionally creates a Route53 alias for `slug.baseDomain` → load balancer.
Package: `github.com/ajent-social/pulumi/aws/tenantdnstls`.

Hosted zone and base domain are caller-owned. Never derive SANs from request
`Host` headers.

See [contract](../../contracts/tenant-dns-tls.md).
