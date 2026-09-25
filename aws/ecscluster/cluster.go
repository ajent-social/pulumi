// Package ecscluster creates an ECS cluster with Fargate execution and task roles.
package ecscluster

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ecs"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

var nameRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}[a-z0-9]$`)

// Args configures one cluster substrate.
type Args struct {
	Name string
	// Region is required for the execution role's awslogs and ECR region conditions.
	Region string
}

// Cluster holds cluster ARN and IAM role ARNs for containerdeploy.
type Cluster struct {
	pulumi.ResourceState

	ClusterARN       pulumi.StringOutput `pulumi:"clusterArn"`
	ExecutionRoleARN pulumi.StringOutput `pulumi:"executionRoleArn"`
	TaskRoleARN      pulumi.StringOutput `pulumi:"taskRoleArn"`
}

// New creates the ECS cluster and baseline roles.
//
// Execution role: ECR pull + CloudWatch Logs create/put (region-scoped where possible).
// Task role: empty inline policy — callers attach product permissions separately.
func New(ctx *pulumi.Context, name string, args Args, opts ...pulumi.ResourceOption) (*Cluster, error) {
	if ctx == nil {
		return nil, errors.New("Pulumi context is required")
	}
	if err := validateArgs(args); err != nil {
		return nil, err
	}
	component := &Cluster{}
	if err := ctx.RegisterComponentResource("ajent:aws:ecscluster:Cluster", name, component, opts...); err != nil {
		return nil, err
	}
	tags := pulumi.StringMap{"ajent-capability": pulumi.String("delivery.ecs-cluster")}

	cluster, err := ecs.NewCluster(ctx, name+"-cluster", &ecs.ClusterArgs{
		Name: pulumi.String(args.Name),
		Tags: tags,
	}, pulumi.Parent(component))
	if err != nil {
		return nil, fmt.Errorf("ecs cluster: %w", err)
	}

	assume, err := json.Marshal(map[string]any{
		"Version": "2012-10-17",
		"Statement": []map[string]any{{
			"Effect": "Allow",
			"Principal": map[string]string{"Service": "ecs-tasks.amazonaws.com"},
			"Action": "sts:AssumeRole",
		}},
	})
	if err != nil {
		return nil, err
	}

	execRole, err := iam.NewRole(ctx, name+"-exec", &iam.RoleArgs{
		Name:             pulumi.String(args.Name + "-exec"),
		AssumeRolePolicy: pulumi.String(string(assume)),
		Tags:             tags,
	}, pulumi.Parent(component))
	if err != nil {
		return nil, fmt.Errorf("execution role: %w", err)
	}
	execPolicy, err := json.Marshal(map[string]any{
		"Version": "2012-10-17",
		"Statement": []map[string]any{
			{
				"Effect": "Allow",
				"Action": []string{
					"ecr:GetAuthorizationToken",
				},
				"Resource": "*",
				"Condition": map[string]any{
					"StringEquals": map[string]string{"aws:RequestedRegion": args.Region},
				},
			},
			{
				"Effect": "Allow",
				"Action": []string{
					"ecr:BatchCheckLayerAvailability",
					"ecr:GetDownloadUrlForLayer",
					"ecr:BatchGetImage",
				},
				"Resource": "*",
				"Condition": map[string]any{
					"StringEquals": map[string]string{"aws:RequestedRegion": args.Region},
				},
			},
			{
				"Effect": "Allow",
				"Action": []string{
					"logs:CreateLogStream",
					"logs:PutLogEvents",
					"logs:CreateLogGroup",
				},
				"Resource": "*",
				"Condition": map[string]any{
					"StringEquals": map[string]string{"aws:RequestedRegion": args.Region},
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}
	if _, err := iam.NewRolePolicy(ctx, name+"-exec-policy", &iam.RolePolicyArgs{
		Role:   execRole.Name,
		Policy: pulumi.String(string(execPolicy)),
	}, pulumi.Parent(component)); err != nil {
		return nil, fmt.Errorf("execution policy: %w", err)
	}

	taskRole, err := iam.NewRole(ctx, name+"-task", &iam.RoleArgs{
		Name:             pulumi.String(args.Name + "-task"),
		AssumeRolePolicy: pulumi.String(string(assume)),
		Tags:             tags,
	}, pulumi.Parent(component))
	if err != nil {
		return nil, fmt.Errorf("task role: %w", err)
	}

	component.ClusterARN = cluster.Arn
	component.ExecutionRoleARN = execRole.Arn
	component.TaskRoleARN = taskRole.Arn
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"clusterArn":       component.ClusterARN,
		"executionRoleArn": component.ExecutionRoleARN,
		"taskRoleArn":      component.TaskRoleARN,
	}); err != nil {
		return nil, err
	}
	return component, nil
}

// ValidateArgs exports argument checks for unit tests.
func ValidateArgs(a Args) error { return validateArgs(a) }

func validateArgs(a Args) error {
	if !nameRE.MatchString(a.Name) {
		return errors.New("Name must be a short lowercase identifier")
	}
	if a.Region == "" {
		return errors.New("Region is required")
	}
	return nil
}
