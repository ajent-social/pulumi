# aws/nlbedge

Internet-facing TLS Network Load Balancer for TCP/gRPC backends.

```go
edge, err := nlbedge.New(ctx, "edge", nlbedge.Args{
    Name:           "app-grpc",
    VPCID:          vpcID,
    SubnetIDs:      publicSubnets,
    CertificateARN: certARN,
    TargetPort:     8080,
})
```

Attach services with `containerdeploy` `TargetGroupARN: edge.TargetGroupARN`.
See [contracts/nlb-edge.md](../../contracts/nlb-edge.md). Use `httpsedge` for
HTTP(S) application load balancing.
