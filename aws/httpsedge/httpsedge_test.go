package httpsedge_test

import (
	"testing"

	"github.com/ajent-social/pulumi/aws/httpsedge"
)

func TestValidateArgs(t *testing.T) {
	err := httpsedge.ValidateArgs(httpsedge.Args{
		Name: "app-edge", VPCID: "vpc-abc123",
		SubnetIDs: []string{"subnet-a", "subnet-b"},
		SecurityGroupIDs: []string{"sg-abc123"},
		CertificateARN: "arn:aws:acm:us-west-2:123456789012:certificate/uuid",
		TargetPort: 8080, HealthCheckPath: "/healthz",
	})
	if err != nil {
		t.Fatal(err)
	}
	err = httpsedge.ValidateArgs(httpsedge.Args{
		Name: "app-edge", VPCID: "vpc-abc123", Internal: true,
		SubnetIDs: []string{"subnet-a", "subnet-b"},
		SecurityGroupIDs: []string{"sg-abc123"},
		CertificateARN: "arn:aws:acm:us-west-2:123456789012:certificate/uuid",
		TargetPort: 8080,
	})
	if err == nil {
		t.Fatal("expected reject internal")
	}
}

func TestValidateTLSPolicy(t *testing.T) {
	if err := httpsedge.ValidateTLSPolicy(""); err != nil {
		t.Fatal(err)
	}
	if err := httpsedge.ValidateTLSPolicy("ELBSecurityPolicy-TLS-1-0-2015-04"); err == nil {
		t.Fatal("expected reject weak policy")
	}
}
