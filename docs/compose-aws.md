# Compose: standards-compliant AWS substrate

Status: documentation only. No sample stack applies cloud resources.

Wire AMSL AWS components in this order for a private Fargate service behind
HTTPS:

1. **Account once:** `oidcprovider.New` then `deploymentidentity` with exact
   subjects and least-privilege policy JSON.
2. **Network:** `network.New` with two AZs, caller CIDRs, `EnableNAT: true`
   for private ECR pulls, `AppPort` / `DataPort` for baseline SGs.
3. **Registry:** `ecrrepo.New` (immutable + scan-on-push); CI pushes
   `@sha256:…` digests only.
4. **Secrets:** `appsecrets.New` shells; never pass plaintext into Args.
5. **Data:** `privatedatabase.New` on private subnet IDs + data SG.
6. **Edge:** `tenantdnstls.New` for ACM, then `httpsedge.New` with public
   subnets, edge SG, and certificate ARN.
7. **Compute:** ECS cluster + task/exec roles (caller), then
   `containerdeploy.New` on private subnets + app SG with digest image.
   Attach the service to the edge target group with an ECS load balancer
   block in the product stack (not yet a separate AMSL component).

Fail closed: no Host-derived trust, no mutable image tags, no public RDS,
no plaintext secrets in Pulumi config for appsecrets.
