// Package nlbedge constructs an internet-facing TLS Network Load Balancer.
package nlbedge

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/lb"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

const defaultAlpn = "HTTP2Preferred"

var (
	nameRE   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,27}[a-z0-9]$`)
	vpcRE    = regexp.MustCompile(`^vpc-[0-9a-f]+$`)
	subnetRE = regexp.MustCompile(`^subnet-[0-9a-f]+$`)
	arnRE    = regexp.MustCompile(`^arn:aws:acm:[a-z0-9-]+:[0-9]{12}:certificate/.+`)
)

// Args configures one TLS NLB edge for TCP/gRPC backends.
type Args struct {
	Name             string
	VPCID            string
	SubnetIDs        []string // public subnets; min 2
	CertificateARN   string   // required unless SkipTLSListener
	TargetPort       int
	HealthCheckPath  string // HTTP health on the target port; default /healthz
	AlpnPolicy       string // default HTTP2Preferred
	PreserveClientIP bool   // default false
	ProxyProtocolV2  bool   // default false; enable only after backends parse PROXY
	AllowDeletion    bool
	Internal         bool // must stay false; true rejected
	SkipTLSListener  bool // when true, create NLB+TG only (cert bootstrap)
}

// Edge holds NLB outputs for DNS and service attachment.
type Edge struct {
	pulumi.ResourceState

	LoadBalancerARN       pulumi.StringOutput `pulumi:"loadBalancerArn"`
	DNSName               pulumi.StringOutput `pulumi:"dnsName"`
	CanonicalHostedZoneID pulumi.StringOutput `pulumi:"canonicalHostedZoneId"`
	TargetGroupARN        pulumi.StringOutput `pulumi:"targetGroupArn"`
	TLSListenerARN        pulumi.StringOutput `pulumi:"tlsListenerArn"`
}

// New creates the NLB, TCP target group, and optional TLS :443 listener.
func New(ctx *pulumi.Context, name string, args Args, opts ...pulumi.ResourceOption) (*Edge, error) {
	if ctx == nil {
		return nil, errors.New("Pulumi context is required")
	}
	if err := validateArgs(args); err != nil {
		return nil, err
	}
	component := &Edge{}
	if err := ctx.RegisterComponentResource("ajent:aws:nlbedge:Edge", name, component, opts...); err != nil {
		return nil, err
	}
	tags := pulumi.StringMap{"ajent-capability": pulumi.String("infrastructure.nlb-edge")}
	alpn := args.AlpnPolicy
	if alpn == "" {
		alpn = defaultAlpn
	}
	path := args.HealthCheckPath
	if path == "" {
		path = "/healthz"
	}

	nlb, err := lb.NewLoadBalancer(ctx, name+"-nlb", &lb.LoadBalancerArgs{
		Name:                     pulumi.String(args.Name),
		LoadBalancerType:         pulumi.String("network"),
		Internal:                 pulumi.Bool(false),
		Subnets:                  pulumi.ToStringArray(args.SubnetIDs),
		EnableDeletionProtection: pulumi.Bool(!args.AllowDeletion),
		Tags:                     tags,
	}, pulumi.Parent(component))
	if err != nil {
		return nil, fmt.Errorf("load balancer: %w", err)
	}

	tg, err := lb.NewTargetGroup(ctx, name+"-tg", &lb.TargetGroupArgs{
		Name:             pulumi.String(args.Name + "-tg"),
		VpcId:            pulumi.String(args.VPCID),
		Port:             pulumi.Int(args.TargetPort),
		Protocol:         pulumi.String("TCP"),
		TargetType:       pulumi.String("ip"),
		PreserveClientIp: pulumi.String(boolString(args.PreserveClientIP)),
		ProxyProtocolV2:  pulumi.Bool(args.ProxyProtocolV2),
		HealthCheck: &lb.TargetGroupHealthCheckArgs{
			Enabled:            pulumi.Bool(true),
			Protocol:           pulumi.String("HTTP"),
			Port:               pulumi.String("traffic-port"),
			Path:               pulumi.String(path),
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

	component.LoadBalancerARN = nlb.Arn
	component.DNSName = nlb.DnsName
	component.CanonicalHostedZoneID = nlb.ZoneId
	component.TargetGroupARN = tg.Arn

	outputs := pulumi.Map{
		"loadBalancerArn":       component.LoadBalancerARN,
		"dnsName":               component.DNSName,
		"canonicalHostedZoneId": component.CanonicalHostedZoneID,
		"targetGroupArn":        component.TargetGroupARN,
	}

	if !args.SkipTLSListener {
		tls, err := lb.NewListener(ctx, name+"-tls", &lb.ListenerArgs{
			LoadBalancerArn: nlb.Arn,
			Port:            pulumi.Int(443),
			Protocol:        pulumi.String("TLS"),
			CertificateArn:  pulumi.String(args.CertificateARN),
			AlpnPolicy:      pulumi.String(alpn),
			DefaultActions: lb.ListenerDefaultActionArray{
				&lb.ListenerDefaultActionArgs{
					Type:           pulumi.String("forward"),
					TargetGroupArn: tg.Arn,
				},
			},
			Tags: tags,
		}, pulumi.Parent(component))
		if err != nil {
			return nil, fmt.Errorf("tls listener: %w", err)
		}
		component.TLSListenerARN = tls.Arn
		outputs["tlsListenerArn"] = component.TLSListenerARN
	}

	if err := ctx.RegisterResourceOutputs(component, outputs); err != nil {
		return nil, err
	}
	return component, nil
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// ValidateArgs exports argument checks for unit tests without a Pulumi context.
func ValidateArgs(a Args) error { return validateArgs(a) }

func validateArgs(a Args) error {
	if !nameRE.MatchString(a.Name) {
		return errors.New("Name must be a short lowercase identifier")
	}
	if a.Internal {
		return errors.New("Internal load balancers are out of scope for nlbedge")
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
	if a.TargetPort <= 0 || a.TargetPort > 65535 {
		return errors.New("TargetPort must be 1-65535")
	}
	if a.HealthCheckPath != "" && !strings.HasPrefix(a.HealthCheckPath, "/") {
		return errors.New("HealthCheckPath must start with /")
	}
	if a.AlpnPolicy != "" {
		switch a.AlpnPolicy {
		case "HTTP1Only", "HTTP2Only", "HTTP2Optional", "HTTP2Preferred", "None":
		default:
			return fmt.Errorf("unsupported AlpnPolicy %q", a.AlpnPolicy)
		}
	}
	if !a.SkipTLSListener {
		if !arnRE.MatchString(a.CertificateARN) {
			return errors.New("CertificateARN must be an ACM certificate ARN")
		}
	}
	// AWS target group names max 32 chars; we append "-tg".
	if len(a.Name)+len("-tg") > 32 {
		return errors.New("Name too long for target group (max 29 characters)")
	}
	return nil
}
