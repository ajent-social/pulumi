// Package containerdeploy runs an ECS Fargate service from an immutable digest.
package containerdeploy

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ecs"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

var (
	digestRE = regexp.MustCompile(`^[0-9]{12}\.dkr\.ecr\.[a-z0-9-]+\.amazonaws\.com/[a-z0-9._/-]+@sha256:[a-f0-9]{64}$`)
	nameRE   = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

// Args configures one Fargate service.
type Args struct {
	Name             string
	ClusterARN       string
	Image            string // must be repo@sha256:… (ECR)
	CPU              string // "256","512",…
	Memory           string
	ContainerPort    int
	SubnetIDs        []string
	SecurityGroupIDs []string
	ExecutionRoleARN string
	TaskRoleARN      string
	AssignPublicIP   bool // default false; must be explicit true to enable
	DesiredCount     int
	Region           string // AWS region for awslogs; required
}

// Service is a Fargate deployment component.
type Service struct {
	pulumi.ResourceState

	ServiceARN         pulumi.StringOutput `pulumi:"serviceArn"`
	TaskDefinitionARN  pulumi.StringOutput `pulumi:"taskDefinitionArn"`
}

// New registers an ECS task definition and service.
func New(ctx *pulumi.Context, name string, args Args, opts ...pulumi.ResourceOption) (*Service, error) {
	if ctx == nil {
		return nil, errors.New("Pulumi context is required")
	}
	if err := validateArgs(args); err != nil {
		return nil, err
	}
	component := &Service{}
	if err := ctx.RegisterComponentResource("ajent:aws:containerdeploy:Service", name, component, opts...); err != nil {
		return nil, err
	}
	count := args.DesiredCount
	if count <= 0 {
		count = 1
	}

	containerDefs := fmt.Sprintf(`[
  {
    "name": %q,
    "image": %q,
    "essential": true,
    "portMappings": [{"containerPort": %d, "protocol": "tcp"}],
    "logConfiguration": {
      "logDriver": "awslogs",
      "options": {
        "awslogs-group": "/ajent/containerdeploy/%s",
        "awslogs-region": %q,
        "awslogs-stream-prefix": "ecs"
      }
    }
  }
]`, args.Name, args.Image, args.ContainerPort, args.Name, args.Region)

	td, err := ecs.NewTaskDefinition(ctx, name+"-td", &ecs.TaskDefinitionArgs{
		Family:                  pulumi.String(args.Name),
		RequiresCompatibilities: pulumi.StringArray{pulumi.String("FARGATE")},
		NetworkMode:             pulumi.String("awsvpc"),
		Cpu:                     pulumi.String(args.CPU),
		Memory:                  pulumi.String(args.Memory),
		ExecutionRoleArn:        pulumi.String(args.ExecutionRoleARN),
		TaskRoleArn:             pulumi.String(args.TaskRoleARN),
		ContainerDefinitions:    pulumi.String(containerDefs),
		Tags: pulumi.StringMap{
			"ajent-capability": pulumi.String("delivery.container-deploy"),
		},
	}, pulumi.Parent(component))
	if err != nil {
		return nil, fmt.Errorf("task definition: %w", err)
	}

	svc, err := ecs.NewService(ctx, name+"-svc", &ecs.ServiceArgs{
		Name:           pulumi.String(args.Name),
		Cluster:        pulumi.String(args.ClusterARN),
		TaskDefinition: td.Arn,
		DesiredCount:   pulumi.Int(count),
		LaunchType:     pulumi.String("FARGATE"),
		NetworkConfiguration: &ecs.ServiceNetworkConfigurationArgs{
			Subnets:        pulumi.ToStringArray(args.SubnetIDs),
			SecurityGroups: pulumi.ToStringArray(args.SecurityGroupIDs),
			AssignPublicIp: pulumi.Bool(args.AssignPublicIP),
		},
		Tags: pulumi.StringMap{
			"ajent-capability": pulumi.String("delivery.container-deploy"),
		},
	}, pulumi.Parent(component))
	if err != nil {
		return nil, fmt.Errorf("ecs service: %w", err)
	}

	// ECS Service ID is the service ARN in the AWS provider.
	component.ServiceARN = svc.ID().ToStringOutput()
	component.TaskDefinitionARN = td.Arn
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"serviceArn":        component.ServiceARN,
		"taskDefinitionArn": component.TaskDefinitionARN,
	}); err != nil {
		return nil, err
	}
	return component, nil
}

// ValidateArgs exports argument checks for unit tests without a Pulumi context.
func ValidateArgs(a Args) error { return validateArgs(a) }

func validateArgs(a Args) error {
	if !nameRE.MatchString(a.Name) {
		return errors.New("invalid Name")
	}
	if a.ClusterARN == "" || a.ExecutionRoleARN == "" || a.TaskRoleARN == "" {
		return errors.New("ClusterARN, ExecutionRoleARN and TaskRoleARN are required")
	}
	if !digestRE.MatchString(a.Image) {
		return errors.New("Image must be an ECR repository@sha256:digest (mutable tags rejected)")
	}
	if strings.Contains(a.Image, ":latest") {
		return errors.New("mutable :latest tag rejected")
	}
	if a.CPU == "" || a.Memory == "" || a.ContainerPort <= 0 {
		return errors.New("CPU, Memory and ContainerPort are required")
	}
	if len(a.SubnetIDs) == 0 || len(a.SecurityGroupIDs) == 0 {
		return errors.New("SubnetIDs and SecurityGroupIDs are required")
	}
	if strings.TrimSpace(a.Region) == "" {
		return errors.New("Region required for awslogs")
	}
	return nil
}

// ValidateImageDigest rejects mutable tags.
func ValidateImageDigest(image string) error {
	if !digestRE.MatchString(image) {
		return errors.New("image must use digest pinning")
	}
	return nil
}
