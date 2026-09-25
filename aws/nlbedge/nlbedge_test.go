package nlbedge_test

import (
	"testing"

	"github.com/ajent-social/pulumi/aws/nlbedge"
)

func TestValidateArgs(t *testing.T) {
	err := nlbedge.ValidateArgs(nlbedge.Args{
		Name: "app-grpc", VPCID: "vpc-abc123",
		SubnetIDs:      []string{"subnet-a", "subnet-b"},
		CertificateARN: "arn:aws:acm:us-west-2:123456789012:certificate/uuid",
		TargetPort:     8080, HealthCheckPath: "/healthz",
	})
	if err != nil {
		t.Fatal(err)
	}
	err = nlbedge.ValidateArgs(nlbedge.Args{
		Name: "app-grpc", VPCID: "vpc-abc123", Internal: true,
		SubnetIDs:      []string{"subnet-a", "subnet-b"},
		CertificateARN: "arn:aws:acm:us-west-2:123456789012:certificate/uuid",
		TargetPort:     8080,
	})
	if err == nil {
		t.Fatal("expected reject internal")
	}
	err = nlbedge.ValidateArgs(nlbedge.Args{
		Name: "app-grpc", VPCID: "vpc-abc123",
		SubnetIDs:       []string{"subnet-a", "subnet-b"},
		TargetPort:      8080,
		SkipTLSListener: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = nlbedge.ValidateArgs(nlbedge.Args{
		Name: "app-grpc", VPCID: "vpc-abc123",
		SubnetIDs:  []string{"subnet-a", "subnet-b"},
		TargetPort: 8080,
	})
	if err == nil {
		t.Fatal("expected reject missing certificate when listener enabled")
	}
}
