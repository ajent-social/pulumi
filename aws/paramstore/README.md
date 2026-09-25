# aws/paramstore

Creates SSM Parameter Store name shells. Values are populated out of band.

```go
bundle, err := paramstore.New(ctx, "params", paramstore.Args{
    Prefix: "app-prod",
    Params: []paramstore.ParamSpec{
        {Name: "DATABASE_URL"},
        {Name: "AUTH_TOKEN_SECRET", Type: "SecureString"},
    },
    KMSKeyID: "alias/aws/ssm",
})
```

See [contracts/param-store.md](../../contracts/param-store.md). Prefer
`appsecrets` for long-lived application secrets that should not live in SSM.
