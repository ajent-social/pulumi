# infrastructure.vpc-network

Status: CANDIDATE AWS VPC implementation. Package:
`github.com/ajent-social/pulumi/aws/network`.

## Intent

Provision a two-AZ VPC with public and private subnets, an internet gateway,
DNS support/hostnames, and baseline security groups for edge → app → data.
Optional single NAT gateway for private-subnet egress (explicit `EnableNAT`).

## Non-goals

Multi-AZ NAT HA, IPv6, VPC peering/Transit Gateway, VPC endpoints, flow logs,
or product-specific CIDR/admin IP defaults. Callers supply CIDRs and AZs.

## Security limits

Private subnets have no internet route unless `EnableNAT` is true. Without NAT
or VPC endpoints, private Fargate tasks cannot pull from ECR unless
`AssignPublicIP` is explicitly enabled on the service. The component does not
attach a Pulumi policy pack.
