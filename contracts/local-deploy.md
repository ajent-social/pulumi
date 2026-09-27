# delivery.local-deploy

Status: DISCOVERED. Command: `github.com/ajent-social/pulumi/cmd/amsl-deploy`.
A potentially reusable boundary with an implementation for review; no
extraction commitment, no verified consumer.

## Intent

Deploy digest-pinned container images to an existing Pulumi stack from an
operator's machine when hosted CI cannot run the deploy. One declarative
config file, checked into the consumer repository, drives a fixed sequence:

1. **Preflight.** Read `HEAD` of the config file's git tree and refuse unless
   it is a full commit SHA, matches `--expect-sha` when given, and the tree is
   clean (`--allow-dirty` overrides and is recorded). Refuse unless the AWS
   caller identity is the configured account. When the pin pull request is
   enabled, also refuse unless the Pulumi repository's stack settings file is
   unmodified and its `HEAD` is on the remote default branch.
2. **Gate.** Run the configured command with a timeout and
   `AMSL_DEPLOY_SOURCE_SHA` in its environment. A non-zero exit refuses the
   deploy before anything is built. The command may run the suite anywhere,
   including as a job on a remote host; see
   [remote build and gate](../docs/local-deploy-remote.md).
3. **Build.** Per image, for its declared platforms, labelled
   `org.opencontainers.image.revision=<sha>`, with the literal `{source_sha}`
   in a `build_args` value replaced by the SHA. Builder `buildx` (default)
   runs `docker buildx build --push` locally. Builder `buildkit` runs
   `buildctl build` against a remote BuildKit daemon: the build context
   streams from this machine and registry credentials are answered over the
   BuildKit session, so neither is stored on the build host.
4. **Push.** Get a short-lived ECR token from the AWS SDK and push under a
   unique `<sha>-<utc timestamp>` tag. With `buildx` the token goes to
   `docker login` over stdin, which stores it in the operator's Docker
   credential store. With `buildkit` it is written only to a private
   temporary Docker config (directory `0700`, file `0600`) that `buildctl`
   reads and that is removed after the build. The token never appears in
   argv, logs or the run record. Then require the digest ECR holds for that
   tag to equal the digest the builder reported. Only `repository@sha256:…`
   references are ever pinned; a tag never is.
5. **Pin.** Record the current value of every configured stack config key,
   then set each key to its new reference through the Pulumi Automation API
   over the consumer's own project directory. No other key is touched.
6. **Preview and confirm.** Run a Pulumi preview with diff, print the
   old-to-new pin and change counts, and require a literal `yes` on an
   interactive terminal or `--yes`. Declining, or no terminal without `--yes`,
   restores the previous config and applies nothing.
7. **Apply.** `pulumi up`.
8. **Verify.** Within `verify.timeout`: wait for each ECS service with the
   SDK `ServicesStable` waiter, optionally require the primary deployment's
   task definition to include each named image's pinned reference, and poll
   each HTTP check for its status and optional JSON field value (a literal, or
   the source SHA).
9. **Roll back.** If apply or verify fails, restore the previous values
   (removing keys that did not exist), `pulumi up` again, and re-wait for the
   ECS services and status-only HTTP checks. Rollback ignores cancellation of
   the run. A failing rollback prints `ROLLBACK FAILED` and exits 3. Nothing
   is committed or published after a rollback.
10. **Publish pins.** After a verified deploy, unless `pin_pr.enabled` is
    `false`: commit only the stack settings file (`Pulumi.<stack>.yaml`) on a
    new branch `<branch_prefix><run-id>` whose parent is the Pulumi
    repository's `HEAD`, built with git plumbing so the operator's branch and
    working tree are not switched. Push it with an empty lease, so an
    existing branch is never moved, and open a pull request against the
    remote's default branch with `gh`. The default branch is never pushed.
    The settings file is left staged locally so a fast-forward to the merged
    pull request applies cleanly. A publishing failure does not undo the
    deploy; it prints `PINS NOT PUBLISHED` and exits 4.
11. **Record.** Always write `<record_dir>/<run-id>.json` and a run log with
    source SHA, dirty flag, account, stack, gate result, image tags, digests
    and previous references, preview counts, confirmation mode, apply result,
    each verify check, rollback outcome, pin branch and pull request, and
    clock timestamps.

`plan` runs preflight and a preview of the current pins without building,
pinning or applying. `validate` checks the config only.

Exit codes: 0 succeeded or planned; 1 refused, nothing applied; 2 apply or
verify failed and the previous pins were re-applied and waited on; 3 rollback
failed, stack state unknown; 4 deployed and verified, pins not published;
64 usage or invalid config.

## Config

JSON, decoded with unknown fields and trailing data rejected; every
validation error is reported at once. Paths are relative to the config
file's directory; `gate.dir`, image `context`/`dockerfile` and `record_dir`
may not leave it. `pulumi.work_dir` may be a relative path to a sibling
repository.

| Field | Rule |
| --- | --- |
| `version` | `1` |
| `aws.region`, `aws.account_id` | Required; the caller identity must match the account |
| `aws.profile`, `aws.assume_role_arn` | Optional; the role is assumed with the SDK `stscreds` provider |
| `gate.command`, `gate.timeout` | Required argv (no shell) and positive Go duration |
| `images[].name` | Unique DNS label |
| `images[].context`, `dockerfile`, `target`, `build_args` | Build inputs |
| `images[].platforms` | One or more `os/arch[/variant]` |
| `images[].repository` | ECR URI in the configured account and region, no tag or digest |
| `images[].config_key` | Unique fully qualified `namespace:key` |
| `builder.kind` | `buildx` (default) or `buildkit` |
| `builder.addr` | `buildkit` only: `tcp://host:port` or `unix:///path` |
| `builder.tls` | Required for `tcp://`: `ca_cert`, `cert`, `key` (machine-local paths, absolute allowed), optional `server_name` |
| `pulumi.work_dir`, `pulumi.stack` | Existing project and stack; the stack is selected, never created |
| `pin_pr.enabled` | Default `true` |
| `pin_pr.branch_prefix`, `pin_pr.remote` | Default `amsl-deploy/` and `origin` |
| `verify.timeout` | Positive Go duration covering all checks |
| `verify.ecs_services[]` | `cluster`, `service`, optional `images` (image names) |
| `verify.http[]` | `url`, `expect_status`, optional `json_field` (dot path) with exactly one of `json_equals` or `json_equals_source_sha` |
| `record_dir` | Optional; default `.amsl-deploy/runs` |

At least one ECS or HTTP check is required: without one a failed rollout
cannot be detected, so it cannot be rolled back.

## Architecture

Images run only on the CPU architecture they were built for. An ECS Fargate
task definition without `runtimePlatform` runs `X86_64`; running
`linux/arm64`-only images needs `runtimePlatform.cpuArchitecture = ARM64`.
`aws/containerdeploy` does not yet expose `runtimePlatform`, so its tasks run
`X86_64`. An `arm64` build host produces `linux/amd64` images either by
cross-compiling in the Dockerfile (`FROM --platform=$BUILDPLATFORM` with
`TARGETOS`/`TARGETARCH`) or through QEMU emulation registered on the host.
A platform mismatch surfaces as a service that never reaches steady state,
which the verify step catches and rolls back.

## Non-goals

Hosted CI, a scheduler or a deploy queue; creating stacks, repositories,
services or build hosts; image signing or scanning (ECR scan-on-push is the
registry's job, see `ecrrepo`); multi-account or multi-region fan-out;
database migrations; canary or traffic shifting; merging the pin pull
request; providers other than AWS ECR and ECS. Concurrent deploys to one
stack are serialized only by the Pulumi backend's stack lock.

## Security limits

- Credentials come from the operator's AWS SDK default chain, profile or an
  assumed role. The tool never records secret values.
- The account check fails closed before any registry or stack call that
  writes.
- A remote BuildKit daemon executes build steps, usually with elevated
  privileges on its host. `tcp://` builders require mutual TLS; without it
  anyone who can reach the port could run code there. The daemon holds the
  registry token in memory while it pushes.
- The gate command, build contexts and Pulumi program are the consumer's own
  code and run with the operator's privileges. The config is trusted input.
- The confirmation proves an operator was present, not that the change was
  reviewed. `--yes` removes even that.
- The pin pull request records what was deployed; merging it is a separate
  human step. Until it merges, the preflight refuses the next deploy because
  the settings file differs from `HEAD`.
- Run records and logs may contain internal hostnames and account IDs from
  the consumer's config and Pulumi output. Keep `record_dir` out of public
  repositories.

## Evidence and limits

- Unit tests with fakes at every seam (git, gate command, AWS, builder,
  Pulumi stack, ECS, pin publisher) and a local HTTP server establish config
  validation, step ordering, the refusal paths, rollback on apply and verify
  failure, rollback of a key that did not previously exist, rollback after
  cancellation, the rollback-failure outcome, pin publishing only after a
  verified deploy, the run record and the exit-code mapping.
- The `buildctl` argv, the temporary credential file (content, `0600`,
  removal, token absent from argv) and the metadata digest parsing are
  tested against a fake command runner. The `buildx` argv is tested the
  same way.
- The pin pull request is tested against real local git repositories with a
  fake pull-request opener: commit parent and single-file content, no
  checkout change, no default-branch push, no overwrite of an existing
  branch, and refusals for a modified settings file, an unpushed `HEAD`, a
  missing settings file and a `HEAD` that moved during the deploy.
- The AWS SDK, Automation API, `docker buildx`, `buildctl` against a live
  daemon and `gh pr create` are **not** exercised by any test here.
- No run against a real AWS account, registry, build host or Pulumi stack
  has been made. No consumer has adopted the tool.

Adoption gate: human review of the flow and exit codes, a disposable-stack run
covering success, verify-failure rollback and plan mode, and one real
consumer deploy recorded with its run record.
