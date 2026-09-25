# infrastructure.https-edge

Status: CANDIDATE AWS Application Load Balancer implementation. Package:
`github.com/ajent-social/pulumi/aws/httpsedge`.

## Intent

Internet-facing ALB with HTTPS (TLS 1.2+ policy), HTTP→HTTPS redirect, and an
IP-mode target group suitable for Fargate `awsvpc`. Certificate ARN and public
subnets are caller-supplied.

## Non-goals

NLB/gRPC, WAF, CloudFront, path-based multi-service routing beyond one target
group, or issuing certificates (see `infrastructure.tenant-dns-tls`).

## Security limits

`DropInvalidHeaderFields` is enabled. Deletion protection defaults on unless
`AllowDeletion` is explicit. The component does not prove live TLS negotiation.
