// Package privatedatabase constructs a private, encrypted AWS RDS instance.
package privatedatabase

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/rds"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

var (
	subnetID = regexp.MustCompile(`^subnet-[0-9a-f]+$`)
	nameRE   = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9-]*$`)
)

// Args configures one private RDS instance.
type Args struct {
	Name                 string
	Engine               string // postgres | mysql
	EngineVersion        string
	InstanceClass        string
	AllocatedStorageGB   int
	SubnetIDs            []string // private subnets only; min 2
	VPCSecurityGroupIDs  []string
	MasterUsername       string
	DBName               string
	BackupRetentionDays  int
	DeletionProtection   bool
	SkipFinalSnapshot    bool // must be false when DeletionProtection is false unless ExplicitSkipFinalSnapshot
	ExplicitSkipFinalSnapshot bool
	KMSKeyID             string // optional CMK; empty uses AWS managed key via StorageEncrypted
}

// PrivateDatabase is a component wrapping RDS with public access denied.
type PrivateDatabase struct {
	pulumi.ResourceState

	InstanceARN         pulumi.StringOutput `pulumi:"instanceArn"`
	Endpoint            pulumi.StringOutput `pulumi:"endpoint"`
	Port                pulumi.IntOutput    `pulumi:"port"`
	SubnetGroupName     pulumi.StringOutput `pulumi:"subnetGroupName"`
	MasterUserSecretARN pulumi.StringOutput `pulumi:"masterUserSecretArn"`
}

// New creates a private encrypted RDS instance.
func New(ctx *pulumi.Context, name string, args Args, opts ...pulumi.ResourceOption) (*PrivateDatabase, error) {
	if ctx == nil {
		return nil, errors.New("Pulumi context is required")
	}
	if err := validateArgs(args); err != nil {
		return nil, err
	}
	component := &PrivateDatabase{}
	if err := ctx.RegisterComponentResource("ajent:aws:privatedatabase:PrivateDatabase", name, component, opts...); err != nil {
		return nil, err
	}
	sg, err := rds.NewSubnetGroup(ctx, name+"-subnets", &rds.SubnetGroupArgs{
		Name:      pulumi.String(args.Name + "-private"),
		SubnetIds: pulumi.ToStringArray(args.SubnetIDs),
		Tags: pulumi.StringMap{
			"ajent-capability": pulumi.String("infrastructure.private-database"),
		},
	}, pulumi.Parent(component))
	if err != nil {
		return nil, fmt.Errorf("subnet group: %w", err)
	}

	skipFinal := args.SkipFinalSnapshot
	if args.DeletionProtection {
		skipFinal = false
	}

	instArgs := &rds.InstanceArgs{
		Identifier:               pulumi.String(args.Name),
		Engine:                   pulumi.String(args.Engine),
		EngineVersion:            pulumi.String(args.EngineVersion),
		InstanceClass:            pulumi.String(args.InstanceClass),
		AllocatedStorage:         pulumi.Int(args.AllocatedStorageGB),
		DbSubnetGroupName:        sg.Name,
		VpcSecurityGroupIds:      pulumi.ToStringArray(args.VPCSecurityGroupIDs),
		Username:                 pulumi.String(args.MasterUsername),
		ManageMasterUserPassword: pulumi.Bool(true),
		DbName:                   pulumi.String(args.DBName),
		PubliclyAccessible:       pulumi.Bool(false),
		StorageEncrypted:         pulumi.Bool(true),
		BackupRetentionPeriod:    pulumi.Int(args.BackupRetentionDays),
		DeletionProtection:       pulumi.Bool(args.DeletionProtection),
		SkipFinalSnapshot:        pulumi.Bool(skipFinal),
		Tags: pulumi.StringMap{
			"ajent-capability": pulumi.String("infrastructure.private-database"),
		},
	}
	if args.KMSKeyID != "" {
		instArgs.KmsKeyId = pulumi.String(args.KMSKeyID)
	}

	inst, err := rds.NewInstance(ctx, name+"-db", instArgs, pulumi.Parent(component))
	if err != nil {
		return nil, fmt.Errorf("rds instance: %w", err)
	}

	component.InstanceARN = inst.Arn
	component.Endpoint = inst.Endpoint
	component.Port = inst.Port
	component.SubnetGroupName = sg.Name
	component.MasterUserSecretARN = inst.MasterUserSecrets.Index(pulumi.Int(0)).SecretArn().Elem()
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"instanceArn":         component.InstanceARN,
		"endpoint":            component.Endpoint,
		"port":                component.Port,
		"subnetGroupName":     component.SubnetGroupName,
		"masterUserSecretArn": component.MasterUserSecretARN,
	}); err != nil {
		return nil, err
	}
	return component, nil
}

// ValidateArgs exports argument checks for unit tests without a Pulumi context.
func ValidateArgs(a Args) error { return validateArgs(a) }

func validateArgs(a Args) error {
	if !nameRE.MatchString(a.Name) || len(a.Name) > 60 {
		return errors.New("Name must be a short alphanumeric identifier")
	}
	switch strings.ToLower(a.Engine) {
	case "postgres", "mysql":
	default:
		return errors.New("Engine must be postgres or mysql")
	}
	if a.EngineVersion == "" || a.InstanceClass == "" || a.MasterUsername == "" || a.DBName == "" {
		return errors.New("EngineVersion, InstanceClass, MasterUsername and DBName are required")
	}
	if a.AllocatedStorageGB < 20 {
		return errors.New("AllocatedStorageGB must be >= 20")
	}
	if len(a.SubnetIDs) < 2 {
		return errors.New("at least two private SubnetIDs required")
	}
	for _, id := range a.SubnetIDs {
		if !subnetID.MatchString(id) {
			return fmt.Errorf("invalid subnet id %q", id)
		}
	}
	if len(a.VPCSecurityGroupIDs) == 0 {
		return errors.New("VPCSecurityGroupIDs required")
	}
	if a.BackupRetentionDays < 1 {
		return errors.New("BackupRetentionDays must be >= 1")
	}
	if !a.DeletionProtection && a.SkipFinalSnapshot && !a.ExplicitSkipFinalSnapshot {
		return errors.New("SkipFinalSnapshot requires ExplicitSkipFinalSnapshot when DeletionProtection is false")
	}
	return nil
}

// ValidatePublicAccessDenied is a pure check for tests/policy fixtures.
func ValidatePublicAccessDenied(publiclyAccessible bool) error {
	if publiclyAccessible {
		return errors.New("publicly accessible database denied")
	}
	return nil
}
