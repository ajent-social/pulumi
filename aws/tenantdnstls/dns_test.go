package tenantdnstls_test

import (
	"testing"

	"github.com/ajent-social/pulumi/aws/tenantdnstls"
)

func TestValidateWildcardSAN(t *testing.T) {
	if err := tenantdnstls.ValidateWildcardSAN("zatiti.cloud", "*.zatiti.cloud"); err != nil {
		t.Fatal(err)
	}
	if err := tenantdnstls.ValidateWildcardSAN("zatiti.cloud", "zatiti.cloud"); err == nil {
		t.Fatal("expected reject")
	}
}

func TestValidateArgs(t *testing.T) {
	if err := tenantdnstls.ValidateArgs(tenantdnstls.Args{
		Name: "prod", BaseDomain: "*.zatiti.cloud", HostedZoneID: "Z123ABC",
	}); err == nil {
		t.Fatal("expected reject wildcard base")
	}
	if err := tenantdnstls.ValidateArgs(tenantdnstls.Args{
		Name: "prod", BaseDomain: "zatiti.cloud", HostedZoneID: "Z123ABC",
	}); err != nil {
		t.Fatal(err)
	}
}

