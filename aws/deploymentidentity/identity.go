// Package deploymentidentity constructs AWS IAM roles for a narrowly scoped
// GitHub Actions deployment identity.
package deploymentidentity

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

const (
	GitHubOIDCIssuer       = "https://token.actions.githubusercontent.com"
	GitHubOIDCAudience     = "sts.amazonaws.com"
	GitHubOIDCProviderHost = "token.actions.githubusercontent.com"
)

var (
	awsOIDCProviderARN = regexp.MustCompile(`^arn:aws:iam::[0-9]{12}:oidc-provider/token\.actions\.githubusercontent\.com$`)
	githubName         = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	decimalID          = regexp.MustCompile(`^[0-9]+$`)
	rolePrefix         = regexp.MustCompile(`^[A-Za-z0-9+=,.@_-]+$`)
)

// Args describes one GitHub repository and its single allowed execution
// context per role. Set exactly one of Ref or Environment for preview and
// exactly one of ApplyRef or ApplyEnvironment for apply. OwnerID and RepositoryID
// must be supplied together to use GitHub's immutable subject format.
type Args struct {
	RoleNamePrefix    string
	ProviderARN       string
	Audience          string
	RepositoryOwner   string
	RepositoryName    string
	OwnerID           string
	RepositoryID      string
	Ref               string
	Environment       string
	ApplyRef          string
	ApplyEnvironment  string
	PreviewPolicyJSON string
	ApplyPolicyJSON   string
}

// GitHubActionsDeploymentIdentity is a component containing separate preview
// and apply roles, each with an exact GitHub OIDC trust and caller-supplied
// resource permissions.
type GitHubActionsDeploymentIdentity struct {
	pulumi.ResourceState

	PreviewRoleARN  pulumi.StringOutput `pulumi:"previewRoleArn"`
	ApplyRoleARN    pulumi.StringOutput `pulumi:"applyRoleArn"`
	Subject         pulumi.StringOutput `pulumi:"subject"`
	ApplySubject    pulumi.StringOutput `pulumi:"applySubject"`
	TrustPolicyJSON pulumi.StringOutput `pulumi:"trustPolicyJSON"`
}

// NewGitHubActionsDeploymentIdentity creates two separately named roles and
// attaches the explicit caller-supplied preview and apply policies. It does
// not create the account's OIDC provider, add a permissions boundary, or
// choose workflow/environment protection policy.
func NewGitHubActionsDeploymentIdentity(
	ctx *pulumi.Context,
	name string,
	args Args,
	opts ...pulumi.ResourceOption,
) (*GitHubActionsDeploymentIdentity, error) {
	if ctx == nil {
		return nil, errors.New("Pulumi context is required")
	}
	subject, err := validateArgs(name, args)
	if err != nil {
		return nil, err
	}
	applyArgs := args
	applyArgs.Ref, applyArgs.Environment = args.ApplyRef, args.ApplyEnvironment
	applySubject, err := validateArgs(name, applyArgs)
	if err != nil {
		return nil, fmt.Errorf("apply execution context: %w", err)
	}
	if subject == applySubject {
		return nil, errors.New("preview and apply must have different exact execution contexts")
	}
	applyTrustJSON, err := buildTrustPolicy(args.ProviderARN, args.Audience, applySubject)
	if err != nil {
		return nil, err
	}
	trustJSON, err := buildTrustPolicy(args.ProviderARN, args.Audience, subject)
	if err != nil {
		return nil, err
	}

	component := &GitHubActionsDeploymentIdentity{}
	if err := ctx.RegisterComponentResource("ajent:aws:deploymentidentity:GitHubActionsDeploymentIdentity", name, component, opts...); err != nil {
		return nil, fmt.Errorf("register deployment identity component: %w", err)
	}

	preview, err := iam.NewRole(ctx, name+"-preview", &iam.RoleArgs{
		Name:             pulumi.String(args.RoleNamePrefix + "-preview"),
		Description:      pulumi.StringPtr("GitHub Actions deployment preview role"),
		AssumeRolePolicy: pulumi.String(trustJSON),
		Tags: pulumi.StringMap{
			"ajent-capability": pulumi.String("infrastructure.deployment-identity"),
			"ajent-authority":  pulumi.String("preview"),
		},
	}, pulumi.Parent(component))
	if err != nil {
		return nil, fmt.Errorf("create preview IAM role: %w", err)
	}

	apply, err := iam.NewRole(ctx, name+"-apply", &iam.RoleArgs{
		Name:             pulumi.String(args.RoleNamePrefix + "-apply"),
		Description:      pulumi.StringPtr("GitHub Actions deployment apply role"),
		AssumeRolePolicy: pulumi.String(applyTrustJSON),
		Tags: pulumi.StringMap{
			"ajent-capability": pulumi.String("infrastructure.deployment-identity"),
			"ajent-authority":  pulumi.String("apply"),
		},
	}, pulumi.Parent(component))
	if err != nil {
		return nil, fmt.Errorf("create apply IAM role: %w", err)
	}

	if _, err = iam.NewRolePolicy(ctx, name+"-preview-permissions", &iam.RolePolicyArgs{
		Name:   pulumi.StringPtr(args.RoleNamePrefix + "-preview"),
		Role:   preview.Name,
		Policy: pulumi.String(args.PreviewPolicyJSON),
	}, pulumi.Parent(component)); err != nil {
		return nil, fmt.Errorf("attach preview IAM policy: %w", err)
	}
	if _, err = iam.NewRolePolicy(ctx, name+"-apply-permissions", &iam.RolePolicyArgs{
		Name:   pulumi.StringPtr(args.RoleNamePrefix + "-apply"),
		Role:   apply.Name,
		Policy: pulumi.String(args.ApplyPolicyJSON),
	}, pulumi.Parent(component)); err != nil {
		return nil, fmt.Errorf("attach apply IAM policy: %w", err)
	}

	component.PreviewRoleARN = preview.Arn
	component.ApplyRoleARN = apply.Arn
	component.Subject = pulumi.String(subject).ToStringOutput()
	component.ApplySubject = pulumi.String(applySubject).ToStringOutput()
	component.TrustPolicyJSON = pulumi.String(trustJSON).ToStringOutput()
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"previewRoleArn":  component.PreviewRoleARN,
		"applyRoleArn":    component.ApplyRoleARN,
		"subject":         component.Subject,
		"applySubject":    component.ApplySubject,
		"trustPolicyJSON": component.TrustPolicyJSON,
	}); err != nil {
		return nil, fmt.Errorf("register deployment identity outputs: %w", err)
	}
	return component, nil
}

func validateArgs(name string, args Args) (string, error) {
	if name == "" || len(name)+8 > 64 || !rolePrefix.MatchString(name) {
		return "", errors.New("component name must be an IAM-safe role prefix of at most 56 characters")
	}
	if len(args.RoleNamePrefix) == 0 || len(args.RoleNamePrefix)+8 > 64 || !rolePrefix.MatchString(args.RoleNamePrefix) {
		return "", errors.New("role name prefix must be IAM-safe and leave room for role suffixes")
	}
	if !awsOIDCProviderARN.MatchString(args.ProviderARN) {
		return "", errors.New("provider ARN must identify the GitHub Actions OIDC provider in an AWS account")
	}
	if args.Audience != GitHubOIDCAudience {
		return "", fmt.Errorf("audience must be %q", GitHubOIDCAudience)
	}
	if !validGitHubName(args.RepositoryOwner) || !validGitHubName(args.RepositoryName) {
		return "", errors.New("repository owner and name must be explicit GitHub identifiers")
	}
	if (args.OwnerID == "") != (args.RepositoryID == "") {
		return "", errors.New("owner ID and repository ID must be provided together")
	}
	if args.OwnerID != "" && (!decimalID.MatchString(args.OwnerID) || !decimalID.MatchString(args.RepositoryID)) {
		return "", errors.New("immutable GitHub owner and repository IDs must be decimal identifiers")
	}
	if (args.Ref == "") == (args.Environment == "") {
		return "", errors.New("set exactly one of ref or environment")
	}
	if args.Ref != "" && !validGitHubRef(args.Ref) {
		return "", errors.New("ref must be one exact refs/heads/... or refs/tags/... value with no wildcard")
	}
	if args.Environment != "" && !validExactValue(args.Environment) {
		return "", errors.New("environment must be a non-empty exact name with no wildcard")
	}
	subject := buildSubject(args)
	if subject == "" || strings.ContainsAny(subject, "*?") {
		return "", errors.New("computed GitHub subject must be exact and contain no wildcard")
	}
	if err := validatePermissionsPolicy(args.PreviewPolicyJSON); err != nil {
		return "", fmt.Errorf("preview policy: %w", err)
	}
	if err := validatePermissionsPolicy(args.ApplyPolicyJSON); err != nil {
		return "", fmt.Errorf("apply policy: %w", err)
	}
	previewCanonical, err := canonicalJSON(args.PreviewPolicyJSON)
	if err != nil {
		return "", fmt.Errorf("preview policy: %w", err)
	}
	applyCanonical, err := canonicalJSON(args.ApplyPolicyJSON)
	if err != nil {
		return "", fmt.Errorf("apply policy: %w", err)
	}
	if previewCanonical == applyCanonical {
		return "", errors.New("preview and apply policy documents must differ; semantic authority remains caller-owned")
	}
	return subject, nil
}

func validGitHubName(value string) bool {
	return value != "" && githubName.MatchString(value) && !strings.ContainsAny(value, "*?")
}

func validExactValue(value string) bool {
	return strings.TrimSpace(value) == value && value != "" && !strings.ContainsAny(value, "*?\r\n")
}

func validGitHubRef(value string) bool {
	if !validExactValue(value) || strings.Contains(value, ":") || strings.Contains(value, "..") || strings.Contains(value, "//") || strings.HasSuffix(value, "/") {
		return false
	}
	return strings.HasPrefix(value, "refs/heads/") && len(value) > len("refs/heads/") || strings.HasPrefix(value, "refs/tags/") && len(value) > len("refs/tags/")
}

func buildSubject(args Args) string {
	repository := args.RepositoryOwner + "/" + args.RepositoryName
	if args.OwnerID != "" {
		repository = args.RepositoryOwner + "@" + args.OwnerID + "/" + args.RepositoryName + "@" + args.RepositoryID
	}
	if args.Environment != "" {
		return "repo:" + repository + ":environment:" + strings.ReplaceAll(args.Environment, ":", "%3A")
	}
	return "repo:" + repository + ":ref:" + args.Ref
}

func buildTrustPolicy(providerARN, audience, subject string) (string, error) {
	policy := map[string]any{
		"Version": "2012-10-17",
		"Statement": []any{map[string]any{
			"Effect":    "Allow",
			"Principal": map[string]any{"Federated": providerARN},
			"Action":    "sts:AssumeRoleWithWebIdentity",
			"Condition": map[string]any{"StringEquals": map[string]any{
				GitHubOIDCProviderHost + ":aud": audience,
				GitHubOIDCProviderHost + ":sub": subject,
			}},
		}},
	}
	encoded, err := json.Marshal(policy)
	if err != nil {
		return "", fmt.Errorf("encode GitHub OIDC trust policy: %w", err)
	}
	return string(encoded), nil
}
