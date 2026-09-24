package containerdeploy_test

import (
	"testing"

	"github.com/ajent-social/pulumi/aws/containerdeploy"
)

func TestValidateImageDigest(t *testing.T) {
	good := "123456789012.dkr.ecr.us-east-1.amazonaws.com/app@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := containerdeploy.ValidateImageDigest(good); err != nil {
		t.Fatal(err)
	}
	if err := containerdeploy.ValidateImageDigest("123456789012.dkr.ecr.us-east-1.amazonaws.com/app:latest"); err == nil {
		t.Fatal("expected reject latest")
	}
}

func TestValidateArgsRequiresRegion(t *testing.T) {
	err := containerdeploy.ValidateArgs(containerdeploy.Args{
		Name: "app", ClusterARN: "arn:aws:ecs:us-east-1:123456789012:cluster/c",
		Image: "123456789012.dkr.ecr.us-east-1.amazonaws.com/app@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		CPU: "256", Memory: "512", ContainerPort: 8080,
		SubnetIDs: []string{"subnet-abc"}, SecurityGroupIDs: []string{"sg-1"},
		ExecutionRoleARN: "arn:aws:iam::123456789012:role/exec",
		TaskRoleARN:      "arn:aws:iam::123456789012:role/task",
	})
	if err == nil {
		t.Fatal("expected reject missing Region")
	}
}

