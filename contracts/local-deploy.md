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
   caller identity is the configured account.
2. **Gate.** Run the configured command with a timeout and
   `AMSL_DEPLOY_SOURCE_SHA` in its environment. A non-zero exit refuses the
   deploy before anything is built.
3. **Build.** `docker buildx build --push` per image for its declared
   platforms, labelled `org.opencontainers.image.revision=<sha>`. The literal
   `{source_sha}` in a `build_args` value is replaced by the SHA.
4. **Push.** Log in to ECR with a token from the AWS SDK (password over
   stdin), push under a unique `<sha>-<utc timestamp>` tag, then require the
   digest ECR holds for that tag to equal the digest buildx reported. Only
   `repository@sha256:…` references are ever pinned; a tag never is.
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
   the run. A failing rollback prints `ROLLBACK FAILED` and exits 3.
10. **Record.** Always write `<record_dir>/<run-id>.json` and a run log with
    source SHA, dirty flag, account, stack, gate result, image tags, digests
    and previous references, preview counts, confirmation mode, apply result,
    each verify check, rollback outcome and clock timestamps.

`plan` runs preflight and a preview of the current pins without building,
pinning or applying. `validate` checks the config only.

Exit codes: 0 succeeded or planned; 1 refused, nothing applied; 2 apply or
verify failed and the previous pins were re-applied and waited on; 3 rollback
failed, stack state unknown; 64 usage or invalid config.

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
| `pulumi.work_dir`, `pulumi.stack` | Existing project and stack; the stack is selected, never created |
| `verify.timeout` | Positive Go duration covering all checks |
| `verify.ecs_services[]` | `cluster`, `service`, optional `images` (image names) |
| `verify.http[]` | `url`, `expect_status`, optional `json_field` (dot path) with exactly one of `json_equals` or `json_equals_source_sha` |
| `record_dir` | Optional; default `.amsl-deploy/runs` |

At least one ECS or HTTP check is required: without one a failed rollout
cannot be detected, so it cannot be rolled back.

## Non-goals

Hosted CI, a scheduler or a deploy queue; creating stacks, repositories or
services; image signing or scanning (ECR scan-on-push is the registry's job,
see `ecrrepo`); multi-account or multi-region fan-out; database migrations;
canary or traffic shifting; providers other than AWS ECR and ECS. Concurrent
deploys to one stack are serialized only by the Pulumi backend's stack lock.

## Security limits

- Credentials come from the operator's AWS SDK default chain, profile or an
  assumed role. The tool never reads, stores or records secret values; the
  ECR password is passed to `docker login` over stdin. `docker login` stores
  the short-lived token in the operator's Docker credential store.
- The account check fails closed before any registry or stack call that
  writes.
- The gate command, build contexts and Pulumi program are the consumer's own
  code and run with the operator's privileges. The config is trusted input.
- The confirmation proves an operator was present, not that the change was
  reviewed. `--yes` removes even that.
- After a successful run the new pins remain in the stack config file in the
  consumer's Pulumi project; committing them is the operator's step.
- Run records and logs may contain internal hostnames and account IDs from
  the consumer's config and Pulumi output. Keep `record_dir` out of public
  repositories.

## Evidence and limits

- Unit tests with fakes at every seam (git, gate command, AWS, buildx, Pulumi
  stack, ECS) and a local HTTP server establish config validation, step
  ordering, the refusal paths, rollback on apply and verify failure, rollback
  of a key that did not previously exist, rollback after cancellation, the
  rollback-failure outcome, the run record and the exit-code mapping.
- The AWS, buildx and Automation API adapters are thin calls to the upstream
  SDKs and CLI and are **not** exercised by any test here.
- No run against a real AWS account, registry or Pulumi stack has been made.
  No consumer has adopted the tool.

Adoption gate: human review of the flow and exit codes, a disposable-stack run
covering success, verify-failure rollback and plan mode, and one real
consumer deploy recorded with its run record.
