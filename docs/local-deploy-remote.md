# Local deploy: remote build host and gate

`amsl-deploy` can build on a machine other than the operator's, for example a
larger or `arm64` host that only accepts Kubernetes-style manifests. This
page describes the reuse-first pattern. None of it has been run by this
repository; treat the manifests as starting points to review.

## Why BuildKit over a manifest-submitted build job

A build job submitted as a manifest (buildah or kaniko in a pod) needs the
source and a registry credential inside the pod. Manifest APIs commonly
persist the manifest and log container environment and exec arguments, so
neither an environment variable nor an exec argument keeps the credential
secret, and the source has to be fetched with yet another credential.

A BuildKit daemon avoids both. `buildctl` on the operator's machine streams
the build context to the daemon and answers its registry credential request
over the BuildKit session. The operator's manifest only starts the daemon;
builds do not pass through the manifest API. Builder `buildkit` in the
config uses this.

## Run the daemon

Submit a long-running workload that runs `buildkitd` with mutual TLS. Keep the
daemon's certificate files in a dedicated host directory mounted read-only.
Distributing those files is a one-time host setup outside this tool.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: buildkitd
spec:
  replicas: 1
  selector:
    matchLabels: {app: buildkitd}
  template:
    metadata:
      labels: {app: buildkitd}
    spec:
      containers:
        - name: buildkitd
          image: moby/buildkit:v0.33.0 # pin by digest
          args:
            - --addr=tcp://0.0.0.0:1234
            - --tlscacert=/certs/ca.pem
            - --tlscert=/certs/cert.pem
            - --tlskey=/certs/key.pem
          securityContext:
            privileged: true
          ports:
            - containerPort: 1234
              hostPort: 1234
          volumeMounts:
            - name: certs
              mountPath: /certs
              readOnly: true
      volumes:
        - name: certs
          hostPath:
            path: /etc/buildkit/certs
```

`privileged: true` is a real widening of what the pod is granted; call it
out in review. BuildKit's rootless image is an alternative that needs fewer
privileges on hosts that support it.

Then point the config at it:

```json
"builder": {
  "kind": "buildkit",
  "addr": "tcp://builder.example.internal:1234",
  "tls": {
    "ca_cert": "/etc/amsl-deploy/buildkit/ca.pem",
    "cert": "/etc/amsl-deploy/buildkit/client.pem",
    "key": "/etc/amsl-deploy/buildkit/client-key.pem"
  }
}
```

## Platforms

A native build produces the host's architecture. For another architecture,
prefer cross-compiling in the Dockerfile:

```dockerfile
FROM --platform=$BUILDPLATFORM golang:1.25 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /out/app ./cmd/app

FROM gcr.io/distroless/static
COPY --from=build /out/app /app
ENTRYPOINT ["/app"]
```

Only `RUN` steps in stages built for the target platform need emulation.
Where that is unavoidable, the host needs QEMU registered with
`binfmt_misc`, which is host setup outside this tool. Make sure the ECS task
definition's `runtimePlatform` matches the images you deploy; see
[the contract](../contracts/local-deploy.md#architecture).

## Run the gate remotely

`gate.command` is any argv that exits zero only when the suite passes, and it
receives `AMSL_DEPLOY_SOURCE_SHA`. Two patterns:

1. **Test stage on the same BuildKit daemon.** Add a `test` stage to the
   Dockerfile and run it with `buildctl` against the same address and TLS
   files. For example: `buildctl ... build --frontend dockerfile.v0 --local
   context=. --local dockerfile=. --opt target=test --output
   type=cacheonly`. The context streams from the operator's machine, so the
   build host needs no repository access. This fits suites that need no
   external services.
2. **A job submitted through the host's manifest API.** A script renders a
   one-shot pod or job with a unique name that includes the SHA, submits it,
   polls its status until it succeeds or fails, prints its logs and exits
   non-zero unless it succeeded. The job fetches the source at exactly
   `AMSL_DEPLOY_SOURCE_SHA` from a location the host can already read. Put no
   secrets in the manifest, for the reasons above.

A sketch of pattern 2 for an API that accepts manifests at `$API/pods` and
reports a `status` field. Adapt the endpoints, status values and the job's
command to your host.

```sh
#!/bin/sh
set -eu
name="example-gate-$(printf %.12s "$AMSL_DEPLOY_SOURCE_SHA")-$(date -u +%Y%m%d%H%M%S)"
sed -e "s/__NAME__/$name/" -e "s/__SHA__/$AMSL_DEPLOY_SOURCE_SHA/" gate-pod.yaml |
  curl -fsS -X POST "$API/pods" -H 'Content-Type: application/yaml' --data-binary @- >/dev/null
while :; do
  status=$(curl -fsS "$API/pods/$name" | jq -r .status)
  case "$status" in
    completed) curl -fsS "$API/pods/$name/logs"; exit 0 ;;
    failed|preempted) curl -fsS "$API/pods/$name/logs"; exit 1 ;;
  esac
  sleep 10
done
```

Set `gate.timeout` to cover queueing on the host as well as the suite.
