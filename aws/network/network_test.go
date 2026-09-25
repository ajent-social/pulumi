package network_test

import (
	"testing"

	"github.com/ajent-social/pulumi/aws/network"
)

func validArgs() network.Args {
	return network.Args{
		Name:               "app-net",
		VPCCidr:            "10.0.0.0/16",
		AvailabilityZones:  []string{"us-west-2a", "us-west-2b"},
		PublicSubnetCidrs:  []string{"10.0.0.0/24", "10.0.1.0/24"},
		PrivateSubnetCidrs: []string{"10.0.10.0/24", "10.0.11.0/24"},
		EnableNAT:          true,
		AppPort:            8080,
		DataPort:           5432,
	}
}

func TestValidateArgs(t *testing.T) {
	if err := network.ValidateArgs(validArgs()); err != nil {
		t.Fatal(err)
	}
	bad := validArgs()
	bad.AvailabilityZones = []string{"us-west-2a"}
	if err := network.ValidateArgs(bad); err == nil {
		t.Fatal("expected reject single AZ")
	}
	bad = validArgs()
	bad.PrivateSubnetCidrs = []string{"192.168.0.0/24", "192.168.1.0/24"}
	if err := network.ValidateArgs(bad); err == nil {
		t.Fatal("expected reject CIDR outside VPC")
	}
}

func TestValidatePrivateIsolation(t *testing.T) {
	if err := network.ValidatePrivateIsolation(false, true); err == nil {
		t.Fatal("expected deny default route without NAT")
	}
	if err := network.ValidatePrivateIsolation(false, false); err != nil {
		t.Fatal(err)
	}
}
