// Package oidcprovider creates the account GitHub Actions IAM OIDC provider.
package oidcprovider

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

const (
	// GitHubOIDCURL is GitHub Actions' OIDC issuer URL.
	GitHubOIDCURL = "https://token.actions.githubusercontent.com"
	// GitHubOIDCAudience is the audience aws-actions/configure-aws-credentials uses.
	GitHubOIDCAudience = "sts.amazonaws.com"
	// GitHubOIDCThumbprint is a well-formed 40-hex thumbprint required by the
	// CreateOpenIDConnectProvider API. AWS verifies GitHub's chain via its CA
	// bundle (thumbprint check deprecated for this issuer); the value remains
	// API-mandatory. Public source: GitHub changelog 2023-06-27 and AWS IAM docs.
	GitHubOIDCThumbprint = "1c58a3a8518e8759bf075b76b750d4f2df264fcd"
)

var thumbRE = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Args configures the GitHub Actions OIDC provider. Empty fields use the
// documented GitHub defaults.
type Args struct {
	ClientIDs   []string // default: sts.amazonaws.com
	Thumbprints []string // default: GitHubOIDCThumbprint
}

// Provider is the account-level OIDC identity provider.
type Provider struct {
	pulumi.ResourceState

	ARN pulumi.StringOutput `pulumi:"arn"`
	URL pulumi.StringOutput `pulumi:"url"`
}

// New creates the IAM OIDC provider for GitHub Actions.
//
// Exactly one provider may exist per issuer URL per account. Callers must not
// create a second copy in the same account.
func New(ctx *pulumi.Context, name string, args Args, opts ...pulumi.ResourceOption) (*Provider, error) {
	if ctx == nil {
		return nil, errors.New("Pulumi context is required")
	}
	if err := validateArgs(args); err != nil {
		return nil, err
	}
	clients := args.ClientIDs
	if len(clients) == 0 {
		clients = []string{GitHubOIDCAudience}
	}
	thumbs := args.Thumbprints
	if len(thumbs) == 0 {
		thumbs = []string{GitHubOIDCThumbprint}
	}
	component := &Provider{}
	if err := ctx.RegisterComponentResource("ajent:aws:oidcprovider:Provider", name, component, opts...); err != nil {
		return nil, err
	}
	p, err := iam.NewOpenIdConnectProvider(ctx, name+"-github", &iam.OpenIdConnectProviderArgs{
		Url:             pulumi.String(GitHubOIDCURL),
		ClientIdLists:   pulumi.ToStringArray(clients),
		ThumbprintLists: pulumi.ToStringArray(thumbs),
		Tags: pulumi.StringMap{
			"ajent-capability": pulumi.String("infrastructure.github-oidc-provider"),
		},
	}, pulumi.Parent(component))
	if err != nil {
		return nil, fmt.Errorf("oidc provider: %w", err)
	}
	component.ARN = p.Arn
	component.URL = pulumi.String(GitHubOIDCURL).ToStringOutput()
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"arn": component.ARN,
		"url": component.URL,
	}); err != nil {
		return nil, err
	}
	return component, nil
}

// ValidateArgs exports argument checks for unit tests without a Pulumi context.
func ValidateArgs(a Args) error { return validateArgs(a) }

func validateArgs(a Args) error {
	for _, c := range a.ClientIDs {
		if c == "" {
			return errors.New("ClientIDs must not contain empty values")
		}
	}
	if err := ValidateAudience(a.ClientIDs); err != nil {
		return err
	}
	for _, t := range a.Thumbprints {
		if !thumbRE.MatchString(t) {
			return fmt.Errorf("thumbprint %q must be 40 lowercase hex characters", t)
		}
	}
	return nil
}

// ValidateAudience ensures sts.amazonaws.com is present when clients are set.
func ValidateAudience(clients []string) error {
	if len(clients) == 0 {
		return nil
	}
	for _, c := range clients {
		if c == GitHubOIDCAudience {
			return nil
		}
	}
	return errors.New("ClientIDs must include sts.amazonaws.com for GitHub Actions AWS federation")
}
