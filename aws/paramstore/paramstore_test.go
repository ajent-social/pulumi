package paramstore_test

import (
	"testing"

	"github.com/ajent-social/pulumi/aws/paramstore"
)

func TestValidateArgs(t *testing.T) {
	err := paramstore.ValidateArgs(paramstore.Args{
		Prefix: "app-prod",
		Params: []paramstore.ParamSpec{{Name: "DATABASE_URL"}, {Name: "AUTH_TOKEN_SECRET", Type: "SecureString"}},
		KMSKeyID: "alias/aws/ssm",
	})
	if err != nil {
		t.Fatal(err)
	}
	err = paramstore.ValidateArgs(paramstore.Args{
		Prefix: "app-prod",
		Params: []paramstore.ParamSpec{{Name: "a/b"}},
	})
	if err == nil {
		t.Fatal("expected reject nested path")
	}
	err = paramstore.ValidateArgs(paramstore.Args{
		Prefix: "app-prod",
		Params: []paramstore.ParamSpec{{Name: "SECRET", Type: "SecureString"}},
	})
	if err == nil {
		t.Fatal("expected reject SecureString without KMS")
	}
	err = paramstore.ValidateArgs(paramstore.Args{
		Prefix: "app-prod",
		Params: []paramstore.ParamSpec{{Name: "x", Type: "List"}},
	})
	if err == nil {
		t.Fatal("expected reject unsupported Type")
	}
}

func TestRejectPlaintext(t *testing.T) {
	if err := paramstore.RejectPlaintext(true); err == nil {
		t.Fatal("expected reject")
	}
}
