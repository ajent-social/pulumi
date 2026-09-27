# cmd/amsl-deploy

DISCOVERED. Deploy digest-pinned container images to an existing Pulumi
stack from a local machine: preflight, gate, build, push, pin, preview,
apply, verify and roll back on failure.

See [contracts/local-deploy.md](../../contracts/local-deploy.md) for the
behavior, config rules, exit codes and limits.

## Requirements

- Go 1.25 or later to install.
- `git`, `docker` with the buildx plugin, and the `pulumi` CLI on `PATH`.
  Multi-platform builds need a buildx builder that supports every declared
  platform; name it with `-builder`.
- AWS credentials the SDK default chain can find, for the configured account.
- A Pulumi project and existing stack that read the pinned image references
  from stack config.

## Install

```sh
go install github.com/ajent-social/pulumi/cmd/amsl-deploy@latest
```

## Use

```sh
amsl-deploy -config amsl-deploy.json validate
amsl-deploy -config amsl-deploy.json plan
amsl-deploy -config amsl-deploy.json deploy
```

Flags: `-yes` (skip the confirmation), `-allow-dirty`, `-expect-sha <sha>`,
`-builder <name>`.

## Example config

All values are placeholders.

```json
{
  "version": 1,
  "aws": {
    "region": "us-east-1",
    "account_id": "123456789012",
    "profile": "deploy"
  },
  "gate": {
    "command": ["./scripts/test.sh"],
    "timeout": "30m"
  },
  "images": [
    {
      "name": "api",
      "context": ".",
      "dockerfile": "Dockerfile",
      "platforms": ["linux/arm64"],
      "build_args": {"VERSION": "{source_sha}"},
      "repository": "123456789012.dkr.ecr.us-east-1.amazonaws.com/example-api",
      "config_key": "example:apiImage"
    }
  ],
  "pulumi": {
    "work_dir": "infra",
    "stack": "staging"
  },
  "verify": {
    "timeout": "15m",
    "ecs_services": [
      {"cluster": "example", "service": "api", "images": ["api"]}
    ],
    "http": [
      {
        "url": "https://staging.example.com/version",
        "expect_status": 200,
        "json_field": "commit",
        "json_equals_source_sha": true
      }
    ]
  }
}
```

Add the record directory (default `.amsl-deploy/`) to `.gitignore`.
