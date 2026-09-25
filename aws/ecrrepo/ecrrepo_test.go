package ecrrepo_test

import (
	"testing"

	"github.com/ajent-social/pulumi/aws/ecrrepo"
)

func TestValidateArgs(t *testing.T) {
	if err := ecrrepo.ValidateArgs(ecrrepo.Args{Name: "app/api"}); err != nil {
		t.Fatal(err)
	}
	if err := ecrrepo.ValidateArgs(ecrrepo.Args{Name: "Bad Name"}); err == nil {
		t.Fatal("expected reject")
	}
}

func TestValidateImmutability(t *testing.T) {
	if err := ecrrepo.ValidateImmutability("IMMUTABLE"); err != nil {
		t.Fatal(err)
	}
	if err := ecrrepo.ValidateImmutability("MUTABLE"); err == nil {
		t.Fatal("expected reject mutable")
	}
}
