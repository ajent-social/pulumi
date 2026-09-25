// Package network constructs a two-AZ VPC with public/private subnets and
// baseline edge → app → data security groups.
package network

import (
	"errors"
	"fmt"
	"net"
	"regexp"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

var (
	nameRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}[a-z0-9]$`)
	azRE   = regexp.MustCompile(`^[a-z]{2}-[a-z]+-[0-9][a-z]$`)
)

// Args configures one standards-oriented VPC.
type Args struct {
	Name               string
	VPCCidr            string   // e.g. 10.0.0.0/16
	AvailabilityZones  []string // exactly two
	PublicSubnetCidrs  []string // exactly two, within VPC
	PrivateSubnetCidrs []string // exactly two, within VPC
	EnableNAT          bool     // single NAT in first public subnet when true
	AppPort            int      // edge → app
	DataPort           int      // app → data (e.g. 5432)
}

// Network holds VPC outputs for composing other AMSL components.
type Network struct {
	pulumi.ResourceState

	VPCID             pulumi.StringOutput      `pulumi:"vpcId"`
	PublicSubnetIDs   pulumi.StringArrayOutput `pulumi:"publicSubnetIds"`
	PrivateSubnetIDs  pulumi.StringArrayOutput `pulumi:"privateSubnetIds"`
	EdgeSecurityGroup pulumi.StringOutput      `pulumi:"edgeSecurityGroupId"`
	AppSecurityGroup  pulumi.StringOutput      `pulumi:"appSecurityGroupId"`
	DataSecurityGroup pulumi.StringOutput      `pulumi:"dataSecurityGroupId"`
	NATGatewayID      pulumi.StringOutput      `pulumi:"natGatewayId"`
}

// New creates the VPC substrate.
func New(ctx *pulumi.Context, name string, args Args, opts ...pulumi.ResourceOption) (*Network, error) {
	if ctx == nil {
		return nil, errors.New("Pulumi context is required")
	}
	if err := validateArgs(args); err != nil {
		return nil, err
	}
	component := &Network{}
	if err := ctx.RegisterComponentResource("ajent:aws:network:Network", name, component, opts...); err != nil {
		return nil, err
	}
	tags := pulumi.StringMap{"ajent-capability": pulumi.String("infrastructure.vpc-network")}

	vpc, err := ec2.NewVpc(ctx, name+"-vpc", &ec2.VpcArgs{
		CidrBlock:          pulumi.String(args.VPCCidr),
		EnableDnsSupport:   pulumi.Bool(true),
		EnableDnsHostnames: pulumi.Bool(true),
		Tags:               tags,
	}, pulumi.Parent(component))
	if err != nil {
		return nil, fmt.Errorf("vpc: %w", err)
	}

	igw, err := ec2.NewInternetGateway(ctx, name+"-igw", &ec2.InternetGatewayArgs{
		VpcId: vpc.ID(),
		Tags:  tags,
	}, pulumi.Parent(component))
	if err != nil {
		return nil, fmt.Errorf("internet gateway: %w", err)
	}

	publicRT, err := ec2.NewRouteTable(ctx, name+"-public-rt", &ec2.RouteTableArgs{
		VpcId: vpc.ID(),
		Routes: ec2.RouteTableRouteArray{
			&ec2.RouteTableRouteArgs{
				CidrBlock: pulumi.String("0.0.0.0/0"),
				GatewayId: igw.ID(),
			},
		},
		Tags: tags,
	}, pulumi.Parent(component))
	if err != nil {
		return nil, fmt.Errorf("public route table: %w", err)
	}

	var publicIDs, privateIDs []pulumi.StringOutput
	var firstPublic *ec2.Subnet
	var privateRTs []*ec2.RouteTable

	for i := 0; i < 2; i++ {
		pub, err := ec2.NewSubnet(ctx, fmt.Sprintf("%s-public-%d", name, i), &ec2.SubnetArgs{
			VpcId:               vpc.ID(),
			CidrBlock:           pulumi.String(args.PublicSubnetCidrs[i]),
			AvailabilityZone:    pulumi.String(args.AvailabilityZones[i]),
			MapPublicIpOnLaunch: pulumi.Bool(true),
			Tags:                tags,
		}, pulumi.Parent(component))
		if err != nil {
			return nil, fmt.Errorf("public subnet %d: %w", i, err)
		}
		if _, err := ec2.NewRouteTableAssociation(ctx, fmt.Sprintf("%s-public-rta-%d", name, i), &ec2.RouteTableAssociationArgs{
			SubnetId:     pub.ID(),
			RouteTableId: publicRT.ID(),
		}, pulumi.Parent(component)); err != nil {
			return nil, fmt.Errorf("public rta %d: %w", i, err)
		}
		publicIDs = append(publicIDs, pub.ID().ToStringOutput())
		if i == 0 {
			firstPublic = pub
		}

		priv, err := ec2.NewSubnet(ctx, fmt.Sprintf("%s-private-%d", name, i), &ec2.SubnetArgs{
			VpcId:            vpc.ID(),
			CidrBlock:        pulumi.String(args.PrivateSubnetCidrs[i]),
			AvailabilityZone: pulumi.String(args.AvailabilityZones[i]),
			Tags:             tags,
		}, pulumi.Parent(component))
		if err != nil {
			return nil, fmt.Errorf("private subnet %d: %w", i, err)
		}
		privRT, err := ec2.NewRouteTable(ctx, fmt.Sprintf("%s-private-rt-%d", name, i), &ec2.RouteTableArgs{
			VpcId: vpc.ID(),
			Tags:  tags,
		}, pulumi.Parent(component))
		if err != nil {
			return nil, fmt.Errorf("private route table %d: %w", i, err)
		}
		if _, err := ec2.NewRouteTableAssociation(ctx, fmt.Sprintf("%s-private-rta-%d", name, i), &ec2.RouteTableAssociationArgs{
			SubnetId:     priv.ID(),
			RouteTableId: privRT.ID(),
		}, pulumi.Parent(component)); err != nil {
			return nil, fmt.Errorf("private rta %d: %w", i, err)
		}
		privateIDs = append(privateIDs, priv.ID().ToStringOutput())
		privateRTs = append(privateRTs, privRT)
	}

	var natID pulumi.StringOutput
	if args.EnableNAT {
		eip, err := ec2.NewEip(ctx, name+"-nat-eip", &ec2.EipArgs{
			Domain: pulumi.String("vpc"),
			Tags:   tags,
		}, pulumi.Parent(component))
		if err != nil {
			return nil, fmt.Errorf("nat eip: %w", err)
		}
		nat, err := ec2.NewNatGateway(ctx, name+"-nat", &ec2.NatGatewayArgs{
			AllocationId: eip.ID(),
			SubnetId:     firstPublic.ID(),
			Tags:         tags,
		}, pulumi.Parent(component))
		if err != nil {
			return nil, fmt.Errorf("nat gateway: %w", err)
		}
		natID = nat.ID().ToStringOutput()
		for i, rt := range privateRTs {
			if _, err := ec2.NewRoute(ctx, fmt.Sprintf("%s-private-default-%d", name, i), &ec2.RouteArgs{
				RouteTableId:         rt.ID(),
				DestinationCidrBlock: pulumi.String("0.0.0.0/0"),
				NatGatewayId:         nat.ID(),
			}, pulumi.Parent(component)); err != nil {
				return nil, fmt.Errorf("private default route %d: %w", i, err)
			}
		}
	} else {
		natID = pulumi.String("").ToStringOutput()
	}

	edgeSG, err := ec2.NewSecurityGroup(ctx, name+"-edge-sg", &ec2.SecurityGroupArgs{
		VpcId:       vpc.ID(),
		Description: pulumi.String("HTTPS/HTTP edge ingress"),
		Ingress: ec2.SecurityGroupIngressArray{
			&ec2.SecurityGroupIngressArgs{
				Protocol:    pulumi.String("tcp"),
				FromPort:    pulumi.Int(443),
				ToPort:      pulumi.Int(443),
				CidrBlocks:  pulumi.StringArray{pulumi.String("0.0.0.0/0")},
				Description: pulumi.String("HTTPS"),
			},
			&ec2.SecurityGroupIngressArgs{
				Protocol:    pulumi.String("tcp"),
				FromPort:    pulumi.Int(80),
				ToPort:      pulumi.Int(80),
				CidrBlocks:  pulumi.StringArray{pulumi.String("0.0.0.0/0")},
				Description: pulumi.String("HTTP redirect source"),
			},
		},
		Egress: ec2.SecurityGroupEgressArray{
			&ec2.SecurityGroupEgressArgs{
				Protocol:   pulumi.String("-1"),
				FromPort:   pulumi.Int(0),
				ToPort:     pulumi.Int(0),
				CidrBlocks: pulumi.StringArray{pulumi.String("0.0.0.0/0")},
			},
		},
		Tags: tags,
	}, pulumi.Parent(component))
	if err != nil {
		return nil, fmt.Errorf("edge security group: %w", err)
	}

	appSG, err := ec2.NewSecurityGroup(ctx, name+"-app-sg", &ec2.SecurityGroupArgs{
		VpcId:       vpc.ID(),
		Description: pulumi.String("Application ingress from edge only"),
		Ingress: ec2.SecurityGroupIngressArray{
			&ec2.SecurityGroupIngressArgs{
				Protocol:       pulumi.String("tcp"),
				FromPort:       pulumi.Int(args.AppPort),
				ToPort:         pulumi.Int(args.AppPort),
				SecurityGroups: pulumi.StringArray{edgeSG.ID()},
				Description:    pulumi.String("from edge"),
			},
		},
		Egress: ec2.SecurityGroupEgressArray{
			&ec2.SecurityGroupEgressArgs{
				Protocol:   pulumi.String("-1"),
				FromPort:   pulumi.Int(0),
				ToPort:     pulumi.Int(0),
				CidrBlocks: pulumi.StringArray{pulumi.String("0.0.0.0/0")},
			},
		},
		Tags: tags,
	}, pulumi.Parent(component))
	if err != nil {
		return nil, fmt.Errorf("app security group: %w", err)
	}

	dataSG, err := ec2.NewSecurityGroup(ctx, name+"-data-sg", &ec2.SecurityGroupArgs{
		VpcId:       vpc.ID(),
		Description: pulumi.String("Data plane ingress from app only"),
		Ingress: ec2.SecurityGroupIngressArray{
			&ec2.SecurityGroupIngressArgs{
				Protocol:       pulumi.String("tcp"),
				FromPort:       pulumi.Int(args.DataPort),
				ToPort:         pulumi.Int(args.DataPort),
				SecurityGroups: pulumi.StringArray{appSG.ID()},
				Description:    pulumi.String("from app"),
			},
		},
		// No egress rules: fail closed for data plane.
		Egress: ec2.SecurityGroupEgressArray{},
		Tags:   tags,
	}, pulumi.Parent(component))
	if err != nil {
		return nil, fmt.Errorf("data security group: %w", err)
	}

	component.VPCID = vpc.ID().ToStringOutput()
	component.PublicSubnetIDs = pulumi.ToStringArrayOutput(publicIDs)
	component.PrivateSubnetIDs = pulumi.ToStringArrayOutput(privateIDs)
	component.EdgeSecurityGroup = edgeSG.ID().ToStringOutput()
	component.AppSecurityGroup = appSG.ID().ToStringOutput()
	component.DataSecurityGroup = dataSG.ID().ToStringOutput()
	component.NATGatewayID = natID

	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"vpcId":               component.VPCID,
		"publicSubnetIds":     component.PublicSubnetIDs,
		"privateSubnetIds":    component.PrivateSubnetIDs,
		"edgeSecurityGroupId": component.EdgeSecurityGroup,
		"appSecurityGroupId":  component.AppSecurityGroup,
		"dataSecurityGroupId": component.DataSecurityGroup,
		"natGatewayId":        component.NATGatewayID,
	}); err != nil {
		return nil, err
	}
	return component, nil
}

// ValidateArgs exports argument checks for unit tests without a Pulumi context.
func ValidateArgs(a Args) error { return validateArgs(a) }

func validateArgs(a Args) error {
	if !nameRE.MatchString(a.Name) {
		return errors.New("Name must be a short lowercase identifier")
	}
	if _, _, err := net.ParseCIDR(a.VPCCidr); err != nil {
		return errors.New("VPCCidr must be a valid CIDR")
	}
	if len(a.AvailabilityZones) != 2 {
		return errors.New("AvailabilityZones must contain exactly two zones")
	}
	for _, az := range a.AvailabilityZones {
		if !azRE.MatchString(az) {
			return fmt.Errorf("invalid availability zone %q", az)
		}
	}
	if a.AvailabilityZones[0] == a.AvailabilityZones[1] {
		return errors.New("AvailabilityZones must be distinct")
	}
	if len(a.PublicSubnetCidrs) != 2 || len(a.PrivateSubnetCidrs) != 2 {
		return errors.New("PublicSubnetCidrs and PrivateSubnetCidrs require exactly two CIDRs each")
	}
	_, vpcNet, err := net.ParseCIDR(a.VPCCidr)
	if err != nil {
		return errors.New("VPCCidr must be a valid CIDR")
	}
	seen := map[string]struct{}{}
	for _, cidr := range append(append([]string{}, a.PublicSubnetCidrs...), a.PrivateSubnetCidrs...) {
		ip, subnet, err := net.ParseCIDR(cidr)
		if err != nil {
			return fmt.Errorf("invalid subnet CIDR %q", cidr)
		}
		if !vpcNet.Contains(ip) {
			return fmt.Errorf("subnet CIDR %q is outside VPCCidr", cidr)
		}
		ones, _ := subnet.Mask.Size()
		vpcOnes, _ := vpcNet.Mask.Size()
		if ones <= vpcOnes {
			return fmt.Errorf("subnet CIDR %q must be smaller than VPCCidr", cidr)
		}
		if _, ok := seen[cidr]; ok {
			return errors.New("subnet CIDRs must be distinct")
		}
		seen[cidr] = struct{}{}
	}
	if a.AppPort <= 0 || a.AppPort > 65535 {
		return errors.New("AppPort must be 1-65535")
	}
	if a.DataPort <= 0 || a.DataPort > 65535 {
		return errors.New("DataPort must be 1-65535")
	}
	return nil
}

// ValidatePrivateIsolation documents that private subnets lack a default route
// when NAT is disabled.
func ValidatePrivateIsolation(enableNAT bool, hasDefaultRoute bool) error {
	if !enableNAT && hasDefaultRoute {
		return errors.New("private default route requires EnableNAT")
	}
	return nil
}
