package appsecrets_test

import (
	"testing"

	"github.com/ajent-social/pulumi/aws/appsecrets"
)

func TestValidateArgs(t *testing.T) {
	err := appsecrets.ValidateArgs(appsecrets.Args{
		Prefix:  "app-prod",
		Secrets: []appsecrets.SecretSpec{{Name: "db-url"}, {Name: "session"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = appsecrets.ValidateArgs(appsecrets.Args{
		Prefix:  "app-prod",
		Secrets: []appsecrets.SecretSpec{{Name: "a/b"}},
	})
	if err == nil {
		t.Fatal("expected reject nested path")
	}
	err = appsecrets.ValidateArgs(appsecrets.Args{
		Prefix:               "app-prod",
		Secrets:              []appsecrets.SecretSpec{{Name: "x"}},
		RecoveryWindowInDays: 3,
	})
	if err == nil {
		t.Fatal("expected reject short recovery")
	}
}

func TestRejectPlaintext(t *testing.T) {
	if err := appsecrets.RejectPlaintext(true); err == nil {
		t.Fatal("expected reject")
	}
}
