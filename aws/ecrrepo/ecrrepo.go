// Package ecrrepo creates an ECR repository with immutable tags and scan-on-push.
package ecrrepo

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ecr"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

var nameRE = regexp.MustCompile(`^[a-z0-9]+(?:(?:[._/-])[a-z0-9]+)*$`)

// Args configures one ECR repository.
type Args struct {
	Name        string
	ForceDelete bool // disposable stacks only; default false
}

// Repository wraps a hardened ECR repo.
type Repository struct {
	pulumi.ResourceState

	RepositoryURL pulumi.StringOutput `pulumi:"repositoryUrl"`
	RepositoryARN pulumi.StringOutput `pulumi:"repositoryArn"`
	RegistryID    pulumi.StringOutput `pulumi:"registryId"`
}

// New creates the repository.
func New(ctx *pulumi.Context, name string, args Args, opts ...pulumi.ResourceOption) (*Repository, error) {
	if ctx == nil {
		return nil, errors.New("Pulumi context is required")
	}
	if err := validateArgs(args); err != nil {
		return nil, err
	}
	component := &Repository{}
	if err := ctx.RegisterComponentResource("ajent:aws:ecrrepo:Repository", name, component, opts...); err != nil {
		return nil, err
	}
	repo, err := ecr.NewRepository(ctx, name+"-repo", &ecr.RepositoryArgs{
		Name:               pulumi.String(args.Name),
		ImageTagMutability: pulumi.String("IMMUTABLE"),
		ForceDelete:        pulumi.Bool(args.ForceDelete),
		ImageScanningConfiguration: &ecr.RepositoryImageScanningConfigurationArgs{
			ScanOnPush: pulumi.Bool(true),
		},
		Tags: pulumi.StringMap{
			"ajent-capability": pulumi.String("delivery.ecr-repository"),
		},
	}, pulumi.Parent(component))
	if err != nil {
		return nil, fmt.Errorf("ecr repository: %w", err)
	}
	component.RepositoryURL = repo.RepositoryUrl
	component.RepositoryARN = repo.Arn
	component.RegistryID = repo.RegistryId
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"repositoryUrl": component.RepositoryURL,
		"repositoryArn": component.RepositoryARN,
		"registryId":    component.RegistryID,
	}); err != nil {
		return nil, err
	}
	return component, nil
}

// ValidateArgs exports argument checks for unit tests without a Pulumi context.
func ValidateArgs(a Args) error { return validateArgs(a) }

func validateArgs(a Args) error {
	if !nameRE.MatchString(a.Name) || len(a.Name) > 256 {
		return errors.New("Name must be a valid ECR repository name")
	}
	if strings.Contains(a.Name, "..") {
		return errors.New("Name must not contain consecutive dots")
	}
	return nil
}

// ValidateImmutability rejects mutable tag configuration.
func ValidateImmutability(mutability string) error {
	if strings.ToUpper(mutability) != "IMMUTABLE" {
		return errors.New("ImageTagMutability must be IMMUTABLE")
	}
	return nil
}
