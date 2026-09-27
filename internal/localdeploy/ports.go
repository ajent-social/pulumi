package localdeploy

import (
	"context"
	"io"
	"time"
)

// Git reads the source revision of a working tree.
type Git interface {
	Head(ctx context.Context, dir string) (string, error)
	Dirty(ctx context.Context, dir string) (bool, error)
}

// Commander runs a local command; a non-zero exit is an error.
type Commander interface {
	Run(ctx context.Context, dir string, argv, env []string, out io.Writer) error
}

// Cloud is the AWS account and registry surface.
type Cloud interface {
	CallerAccount(ctx context.Context) (string, error)
	// RegistryAuth returns a short-lived registry login for the account.
	RegistryAuth(ctx context.Context) (RegistryAuth, error)
	// ImageDigest returns the digest the registry holds for repository:tag.
	ImageDigest(ctx context.Context, repository, tag string) (string, error)
}

// RegistryAuth is a registry endpoint and credentials. Password is secret.
type RegistryAuth struct {
	Endpoint string
	Username string
	Password string
}

// Builder builds and pushes images.
type Builder interface {
	Login(ctx context.Context, auth RegistryAuth) error
	// BuildPush builds req for its platforms, pushes it and returns the
	// digest the builder reports for the pushed manifest.
	BuildPush(ctx context.Context, req BuildRequest, out io.Writer) (string, error)
}

// BuildRequest is one resolved image build.
type BuildRequest struct {
	Context    string
	Dockerfile string
	Target     string
	Platforms  []string
	BuildArgs  map[string]string
	Labels     map[string]string
	Tag        string // repository:tag
}

// ConfigValue is one stack config entry.
type ConfigValue struct {
	Value  string
	Secret bool
}

// Stack is the Pulumi stack being deployed.
type Stack interface {
	Config(ctx context.Context) (map[string]ConfigValue, error)
	SetConfig(ctx context.Context, key string, v ConfigValue) error
	RemoveConfig(ctx context.Context, key string) error
	// Preview returns the planned change counts by operation.
	Preview(ctx context.Context, out io.Writer) (map[string]int, error)
	Up(ctx context.Context, out io.Writer) error
}

// Services observes ECS services.
type Services interface {
	WaitStable(ctx context.Context, cluster, service string, timeout time.Duration) error
	// PrimaryImages returns the container images of the primary deployment.
	PrimaryImages(ctx context.Context, cluster, service string) ([]string, error)
}

// Confirmer asks the operator to approve the previewed change.
type Confirmer interface {
	Confirm(prompt string) (bool, error)
}
