package dnsalias_test

import (
	"testing"

	"github.com/ajent-social/pulumi/cloudflare/dnsalias"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func TestValidateArgs(t *testing.T) {
	err := dnsalias.ValidateArgs(dnsalias.Args{
		ZoneID: "a7d1301110dc64f17e1c34edfb45bb54",
		Records: []dnsalias.RecordSpec{
			{Name: "app.example.com", Type: "CNAME", Content: pulumi.String("dualstack.alb.amazonaws.com")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = dnsalias.ValidateArgs(dnsalias.Args{
		ZoneID: "not-a-zone-id",
		Records: []dnsalias.RecordSpec{
			{Name: "app", Type: "CNAME", Content: pulumi.String("x")},
		},
	})
	if err == nil {
		t.Fatal("expected reject bad zone")
	}
	err = dnsalias.ValidateArgs(dnsalias.Args{
		ZoneID: "a7d1301110dc64f17e1c34edfb45bb54",
		Records: []dnsalias.RecordSpec{
			{Name: "app", Type: "TXT", Content: pulumi.String("x")},
		},
	})
	if err == nil {
		t.Fatal("expected reject TXT")
	}
}

func TestRejectProxied(t *testing.T) {
	if err := dnsalias.RejectProxied(true); err == nil {
		t.Fatal("expected reject")
	}
}
