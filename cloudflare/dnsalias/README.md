# cloudflare/dnsalias

DNS-only Cloudflare records for service aliases and ACM validation CNAMEs.

```go
bundle, err := dnsalias.New(ctx, "edge-dns", dnsalias.Args{
    ZoneID: zoneID,
    Records: []dnsalias.RecordSpec{{
        Name:    "app.example.com",
        Type:    "CNAME",
        Content: alb.DNSName,
    }},
})

val, err := dnsalias.NewValidationCNAME(ctx, "acm-val", dnsalias.ValidationCNAMEArgs{
    ZoneID:  zoneID,
    Name:    validationName,  // trim trailing dot from ACM
    Content: validationValue,
})
```

See [contracts/cloudflare-dns-alias.md](../../contracts/cloudflare-dns-alias.md).
