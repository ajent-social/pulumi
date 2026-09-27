package localdeploy

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// AWS implements Cloud and Services with the AWS SDK for Go v2, using the
// default credential chain, an optional named profile and an optional role.
type AWS struct {
	sts *sts.Client
	ecr *ecr.Client
	ecs *ecs.Client
}

// NewAWS loads credentials for c.
func NewAWS(ctx context.Context, c AWSConfig) (*AWS, error) {
	opts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(c.Region)}
	if c.Profile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(c.Profile))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, err
	}
	if c.AssumeRoleARN != "" {
		p := stscreds.NewAssumeRoleProvider(sts.NewFromConfig(cfg), c.AssumeRoleARN, func(o *stscreds.AssumeRoleOptions) {
			o.RoleSessionName = "amsl-deploy"
		})
		cfg.Credentials = aws.NewCredentialsCache(p)
	}
	return &AWS{sts: sts.NewFromConfig(cfg), ecr: ecr.NewFromConfig(cfg), ecs: ecs.NewFromConfig(cfg)}, nil
}

// CallerAccount implements Cloud.
func (a *AWS) CallerAccount(ctx context.Context) (string, error) {
	out, err := a.sts.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", err
	}
	return aws.ToString(out.Account), nil
}

// RegistryAuth implements Cloud.
func (a *AWS) RegistryAuth(ctx context.Context) (RegistryAuth, error) {
	out, err := a.ecr.GetAuthorizationToken(ctx, &ecr.GetAuthorizationTokenInput{})
	if err != nil {
		return RegistryAuth{}, err
	}
	if len(out.AuthorizationData) == 0 {
		return RegistryAuth{}, errors.New("ECR returned no authorization data")
	}
	d := out.AuthorizationData[0]
	raw, err := base64.StdEncoding.DecodeString(aws.ToString(d.AuthorizationToken))
	if err != nil {
		return RegistryAuth{}, fmt.Errorf("decode ECR token: %w", err)
	}
	user, pass, ok := strings.Cut(string(raw), ":")
	if !ok {
		return RegistryAuth{}, errors.New("ECR token is not user:password")
	}
	return RegistryAuth{Endpoint: aws.ToString(d.ProxyEndpoint), Username: user, Password: pass}, nil
}

// ImageDigest implements Cloud. repository is the full registry/name form.
func (a *AWS) ImageDigest(ctx context.Context, repository, tag string) (string, error) {
	m := repoRE.FindStringSubmatch(repository)
	if m == nil {
		return "", fmt.Errorf("%q is not an ECR repository URI", repository)
	}
	out, err := a.ecr.DescribeImages(ctx, &ecr.DescribeImagesInput{
		RegistryId:     aws.String(m[1]),
		RepositoryName: aws.String(m[3]),
		ImageIds:       []ecrtypes.ImageIdentifier{{ImageTag: aws.String(tag)}},
	})
	if err != nil {
		return "", err
	}
	if len(out.ImageDetails) != 1 {
		return "", fmt.Errorf("ECR returned %d images for tag %s", len(out.ImageDetails), tag)
	}
	return aws.ToString(out.ImageDetails[0].ImageDigest), nil
}

// WaitStable implements Services with the SDK's ServicesStable waiter.
func (a *AWS) WaitStable(ctx context.Context, cluster, service string, timeout time.Duration) error {
	w := ecs.NewServicesStableWaiter(a.ecs)
	return w.Wait(ctx, &ecs.DescribeServicesInput{Cluster: aws.String(cluster), Services: []string{service}}, timeout)
}

// PrimaryImages implements Services.
func (a *AWS) PrimaryImages(ctx context.Context, cluster, service string) ([]string, error) {
	out, err := a.ecs.DescribeServices(ctx, &ecs.DescribeServicesInput{Cluster: aws.String(cluster), Services: []string{service}})
	if err != nil {
		return nil, err
	}
	if len(out.Services) != 1 {
		return nil, fmt.Errorf("service %s/%s not found", cluster, service)
	}
	var td string
	for _, dep := range out.Services[0].Deployments {
		if aws.ToString(dep.Status) == "PRIMARY" {
			td = aws.ToString(dep.TaskDefinition)
		}
	}
	if td == "" {
		return nil, fmt.Errorf("service %s/%s has no primary deployment", cluster, service)
	}
	def, err := a.ecs.DescribeTaskDefinition(ctx, &ecs.DescribeTaskDefinitionInput{TaskDefinition: aws.String(td)})
	if err != nil {
		return nil, err
	}
	var images []string
	for _, c := range def.TaskDefinition.ContainerDefinitions {
		images = append(images, aws.ToString(c.Image))
	}
	return images, nil
}
