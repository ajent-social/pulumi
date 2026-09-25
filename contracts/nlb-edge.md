# infrastructure.nlb-edge

Status: CANDIDATE AWS Network Load Balancer implementation. Package:
`github.com/ajent-social/pulumi/aws/nlbedge`.

## Intent

Internet-facing NLB with TLS :443 termination (ALPN default `HTTP2Preferred`),
TCP IP-mode target group for Fargate/`awsvpc`, and HTTP health checks on the
traffic port (suitable when the process multiplexes gRPC + `/healthz`).

## Non-goals

ALB/HTTPS application routing (see `httpsedge`), WAF, CloudFront, issuing
certificates (see `tenantdnstls`), or enabling `ProxyProtocolV2` /
`PreserveClientIP` by default.

## Security limits

Deletion protection defaults on unless `AllowDeletion` is explicit. Internal
NLBs are rejected. `ProxyProtocolV2` and `PreserveClientIP` default false —
flip only after backends parse PROXY headers / dual-stack behavior is verified.
`SkipTLSListener` builds NLB+TG without a listener for cert bootstrap.
