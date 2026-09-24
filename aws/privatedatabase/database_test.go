package privatedatabase_test

import (
	"testing"

	"github.com/ajent-social/pulumi/aws/privatedatabase"
)

func TestValidatePublicAccessDenied(t *testing.T) {
	if err := privatedatabase.ValidatePublicAccessDenied(true); err == nil {
		t.Fatal("expected deny")
	}
	if err := privatedatabase.ValidatePublicAccessDenied(false); err != nil {
		t.Fatal(err)
	}
}

func TestValidateArgs(t *testing.T) {
	err := privatedatabase.ValidateArgs(privatedatabase.Args{
		Name: "appdb", Engine: "postgres", EngineVersion: "16.3",
		InstanceClass: "db.t4g.micro", AllocatedStorageGB: 20,
		SubnetIDs: []string{"subnet-abc"}, VPCSecurityGroupIDs: []string{"sg-1"},
		MasterUsername: "app", DBName: "app", BackupRetentionDays: 7,
	})
	if err == nil {
		t.Fatal("expected reject single subnet")
	}
}

