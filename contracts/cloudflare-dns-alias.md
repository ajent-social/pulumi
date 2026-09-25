# infrastructure.cloudflare-dns-alias

Status: CANDIDATE Cloudflare DNS-only record implementation. Package:
`github.com/ajent-social/pulumi/cloudflare/dnsalias`.

## Intent

Create **DNS-only** (`Proxied=false`) Cloudflare records that alias a hostname
to an origin edge (typically an ALB/NLB DNS name) and optional ACM DNS
validation CNAMEs. Zone ownership and API tokens stay with the caller
(`cloudflare:apiToken` provider config). Zones are not created here.

## Non-goals

Proxied/orange-cloud records, zone creation, Page Rules, WAF, SSL mode
overrides, or registrar NS cutover.

## Security limits

`Proxied=true` is rejected. Proxying would terminate visitor TLS at Cloudflare
and require opening origin security groups to Cloudflare edge ranges — a
product decision outside this component. ACM validation CNAMEs must stay
DNS-only or validation never completes.

## Census evidence (2026-09-25)

Repeated in active products: `sirerun/foundation` staging Cloudflare ACM+ALB
alias (DNS-only) and `ajent-social/ajent-social` production DNS-only CNAME/A.
Postiz proxied/zone-create patterns stay product-local.
