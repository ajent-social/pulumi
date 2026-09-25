package oidcprovider_test

import (
	"testing"

	"github.com/ajent-social/pulumi/aws/oidcprovider"
)

func TestValidateArgs(t *testing.T) {
	if err := oidcprovider.ValidateArgs(oidcprovider.Args{}); err != nil {
		t.Fatal(err)
	}
	if err := oidcprovider.ValidateArgs(oidcprovider.Args{
		Thumbprints: []string{"short"},
	}); err == nil {
		t.Fatal("expected reject short thumbprint")
	}
	if err := oidcprovider.ValidateArgs(oidcprovider.Args{
		Thumbprints: []string{oidcprovider.GitHubOIDCThumbprint},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestValidateAudience(t *testing.T) {
	if err := oidcprovider.ValidateAudience([]string{"sts.amazonaws.com"}); err != nil {
		t.Fatal(err)
	}
	if err := oidcprovider.ValidateAudience([]string{"other"}); err == nil {
		t.Fatal("expected reject missing sts audience")
	}
}
