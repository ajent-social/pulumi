// Package httpsedge constructs an internet-facing HTTPS Application Load Balancer.
package httpsedge

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/lb"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

const defaultSSLPolicy = "ELBSecurityPolicy-TLS13-1-2-2021-06"

var (
	nameRE   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,27}[a-z0-9]$`)
	vpcRE    = regexp.MustCompile(`^vpc-[0-9a-f]+$`)
	subnetRE = regexp.MustCompile(`^subnet-[0-9a-f]+$`)
	sgRE     = regexp.MustCompile(`^sg-[0-9a-f]+$`)
	arnRE    = regexp.MustCompile(`^arn:aws:acm:[a-z0-9-]+:[0-9]{12}:certificate/.+`)
)

// Args configures one HTTPS edge.
type Args struct {
	Name              string
	VPCID             string
	SubnetIDs         []string // public subnets; min 2
	SecurityGroupIDs  []string
	CertificateARN    string
	TargetPort        int
	HealthCheckPath   string
	SSLPolicy         string // optional; defaults to TLS1.3 policy
	AllowDeletion     bool   // when false, deletion protection is on
	Internal          bool   // must stay false for internet edge; true rejected
}

// Edge holds ALB outputs for DNS and service attachment.
type Edge struct {
	pulumi.ResourceState

	LoadBalancerARN   pulumi.StringOutput `pulumi:"loadBalancerArn"`
	DNSName           pulumi.StringOutput `pulumi:"dnsName"`
	CanonicalHostedZoneID pulumi.StringOutput `pulumi:"canonicalHostedZoneId"`
	TargetGroupARN    pulumi.StringOutput `pulumi:"targetGroupArn"`
	HTTPSListenerARN  pulumi.StringOutput `pulumi:"httpsListenerArn"`
}

// New creates the ALB, target group, and listeners.
func New(ctx *pulumi.Context, name string, args Args, opts ...pulumi.ResourceOption) (*Edge, error) {
	if ctx == nil {
		return nil, errors.New("Pulumi context is required")
	}
	if err := validateArgs(args); err != nil {
		return nil, err
	}
	component := &Edge{}
	if err := ctx.RegisterComponentResource("ajent:aws:httpsedge:Edge", name, component, opts...); err != nil {
		return nil, err
	}
	tags := pulumi.StringMap{"ajent-capability": pulumi.String("infrastructure.https-edge")}
	policy := args.SSLPolicy
	if policy == "" {
		policy = defaultSSLPolicy
	}
	path := args.HealthCheckPath
	if path == "" {
		path = "/healthz"
	}

	alb, err := lb.NewLoadBalancer(ctx, name+"-alb", &lb.LoadBalancerArgs{
		Name:                     pulumi.String(args.Name),
		LoadBalancerType:         pulumi.String("application"),
		Internal:                 pulumi.Bool(false),
		Subnets:                  pulumi.ToStringArray(args.SubnetIDs),
		SecurityGroups:           pulumi.ToStringArray(args.SecurityGroupIDs),
		DropInvalidHeaderFields:  pulumi.Bool(true),
		EnableDeletionProtection: pulumi.Bool(!args.AllowDeletion),
		Tags:                     tags,
	}, pulumi.Parent(component))
	if err != nil {
		return nil, fmt.Errorf("load balancer: %w", err)
	}

	tg, err := lb.NewTargetGroup(ctx, name+"-tg", &lb.TargetGroupArgs{
		Name:       pulumi.String(args.Name + "-tg"),
		VpcId:      pulumi.String(args.VPCID),
		Port:       pulumi.Int(args.TargetPort),
		Protocol:   pulumi.String("HTTP"),
		TargetType: pulumi.String("ip"),
		HealthCheck: &lb.TargetGroupHealthCheckArgs{
			Enabled:            pulumi.Bool(true),
			Path:               pulumi.String(path),
			Port:               pulumi.String("traffic-port"),
			Protocol:           pulumi.String("HTTP"),
			Matcher:            pulumi.String("200"),
			Interval:           pulumi.Int(15),
			Timeout:            pulumi.Int(5),
			HealthyThreshold:   pulumi.Int(2),
			UnhealthyThreshold: pulumi.Int(2),
		},
		Tags: tags,
	}, pulumi.Parent(component))
	if err != nil {
		return nil, fmt.Errorf("target group: %w", err)
	}

	https, err := lb.NewListener(ctx, name+"-https", &lb.ListenerArgs{
		LoadBalancerArn: alb.Arn,
		Port:            pulumi.Int(443),
		Protocol:        pulumi.String("HTTPS"),
		CertificateArn:  pulumi.String(args.CertificateARN),
		SslPolicy:       pulumi.String(policy),
		DefaultActions: lb.ListenerDefaultActionArray{
			&lb.ListenerDefaultActionArgs{
				Type:           pulumi.String("forward"),
				TargetGroupArn: tg.Arn,
			},
		},
		Tags: tags,
	}, pulumi.Parent(component))
	if err != nil {
		return nil, fmt.Errorf("https listener: %w", err)
	}

	if _, err := lb.NewListener(ctx, name+"-http", &lb.ListenerArgs{
		LoadBalancerArn: alb.Arn,
		Port:            pulumi.Int(80),
		Protocol:        pulumi.String("HTTP"),
		DefaultActions: lb.ListenerDefaultActionArray{
			&lb.ListenerDefaultActionArgs{
				Type: pulumi.String("redirect"),
				Redirect: &lb.ListenerDefaultActionRedirectArgs{
					Port:       pulumi.String("443"),
					Protocol:   pulumi.String("HTTPS"),
					StatusCode: pulumi.String("HTTP_301"),
				},
			},
		},
		Tags: tags,
	}, pulumi.Parent(component)); err != nil {
		return nil, fmt.Errorf("http redirect listener: %w", err)
	}

	component.LoadBalancerARN = alb.Arn
	component.DNSName = alb.DnsName
	component.CanonicalHostedZoneID = alb.ZoneId
	component.TargetGroupARN = tg.Arn
	component.HTTPSListenerARN = https.Arn

	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"loadBalancerArn":         component.LoadBalancerARN,
		"dnsName":                 component.DNSName,
		"canonicalHostedZoneId":   component.CanonicalHostedZoneID,
		"targetGroupArn":          component.TargetGroupARN,
		"httpsListenerArn":        component.HTTPSListenerARN,
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
	if a.Internal {
		return errors.New("Internal load balancers are out of scope for httpsedge")
	}
	if !vpcRE.MatchString(a.VPCID) {
		return errors.New("VPCID must be a vpc- id")
	}
	if len(a.SubnetIDs) < 2 {
		return errors.New("at least two public SubnetIDs required")
	}
	for _, id := range a.SubnetIDs {
		if !subnetRE.MatchString(id) {
			return fmt.Errorf("invalid subnet id %q", id)
		}
	}
	if len(a.SecurityGroupIDs) == 0 {
		return errors.New("SecurityGroupIDs required")
	}
	for _, id := range a.SecurityGroupIDs {
		if !sgRE.MatchString(id) {
			return fmt.Errorf("invalid security group id %q", id)
		}
	}
	if !arnRE.MatchString(a.CertificateARN) {
		return errors.New("CertificateARN must be an ACM certificate ARN")
	}
	if a.TargetPort <= 0 || a.TargetPort > 65535 {
		return errors.New("TargetPort must be 1-65535")
	}
	if a.HealthCheckPath != "" && !strings.HasPrefix(a.HealthCheckPath, "/") {
		return errors.New("HealthCheckPath must start with /")
	}
	if a.SSLPolicy != "" && !strings.HasPrefix(a.SSLPolicy, "ELBSecurityPolicy-") {
		return errors.New("SSLPolicy must be an ELBSecurityPolicy-* name")
	}
	if err := ValidateTLSPolicy(a.SSLPolicy); err != nil {
		return err
	}
	// AWS target group names max 32 chars; we append "-tg".
	if len(a.Name)+len("-tg") > 32 {
		return errors.New("Name too long for target group (max 29 characters)")
	}
	return nil
}

// ValidateTLSPolicy rejects known-weak policies.
func ValidateTLSPolicy(policy string) error {
	if policy == "" {
		policy = defaultSSLPolicy
	}
	weak := []string{
		"ELBSecurityPolicy-2016-08",
		"ELBSecurityPolicy-TLS-1-0-2015-04",
		"ELBSecurityPolicy-TLS-1-1-2017-01",
	}
	for _, w := range weak {
		if policy == w {
			return fmt.Errorf("weak SSL policy %q rejected", policy)
		}
	}
	return nil
}
