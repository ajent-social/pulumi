package ecscluster_test

import (
	"testing"

	"github.com/ajent-social/pulumi/aws/ecscluster"
)

func TestValidateArgs(t *testing.T) {
	if err := ecscluster.ValidateArgs(ecscluster.Args{Name: "app", Region: "us-west-2"}); err != nil {
		t.Fatal(err)
	}
	if err := ecscluster.ValidateArgs(ecscluster.Args{Name: "app"}); err == nil {
		t.Fatal("expected reject missing region")
	}
}
