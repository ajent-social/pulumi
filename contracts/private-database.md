# infrastructure.private-database

Status: CANDIDATE AWS RDS implementation for review. Package:
`github.com/ajent-social/pulumi/aws/privatedatabase`.

## Intent

Provision an encrypted RDS instance with **no public accessibility**, subnet
group bound to private subnets, and an explicit master secret ARN output
(never plaintext). Engine, size, retention and deletion protection are
caller choices.

## Security limits

The component does not create VPCs, choose cost tiers, prove restore drills,
or attach a Pulumi policy pack. Live connectivity and restore evidence remain
caller responsibilities.
