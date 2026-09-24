# AWS private database

Status: CANDIDATE. Capability: `infrastructure.private-database`.

Constructs an encrypted RDS instance with `PubliclyAccessible=false`, a
private subnet group (≥2 subnets), managed master password in Secrets Manager,
and explicit backup/deletion flags. Package:
`github.com/ajent-social/pulumi/aws/privatedatabase`.

Does not create VPCs, prove restore drills, or attach a Pulumi policy pack.
Live connectivity evidence remains with the caller.

See [contract](../../contracts/private-database.md).
