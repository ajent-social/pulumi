// Package tenantdnstls issues a wildcard ACM certificate and optional slug DNS.
package tenantdnstls

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/acm"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/route53"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

var (
	domainRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
	slugRE   = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	zoneRE   = regexp.MustCompile(`^Z[A-Z0-9]+$`)
)

// Args configures wildcard TLS for a product base domain.
type Args struct {
	Name         string
	BaseDomain   string // e.g. zatiti.cloud — cert for *.BaseDomain and BaseDomain
	HostedZoneID string
	// Optional alias target when creating slug.BaseDomain
	Slug               string
	LoadBalancerDNS    string
	LoadBalancerZoneID string
}

// Bundle holds certificate and optional record outputs.
type Bundle struct {
	pulumi.ResourceState

	CertificateARN pulumi.StringOutput `pulumi:"certificateArn"`
	FQDN           pulumi.StringOutput `pulumi:"fqdn"`
}

// New creates a DNS-validated ACM cert for *.baseDomain.
//
// Apex + wildcard typically share one ACM DNS validation CNAME; this component
// creates that first validation record and waits on CertificateValidation.
func New(ctx *pulumi.Context, name string, args Args, opts ...pulumi.ResourceOption) (*Bundle, error) {
	if ctx == nil {
		return nil, errors.New("Pulumi context is required")
	}
	if err := validateArgs(args); err != nil {
		return nil, err
	}
	component := &Bundle{}
	if err := ctx.RegisterComponentResource("ajent:aws:tenantdnstls:Bundle", name, component, opts...); err != nil {
		return nil, err
	}
	wildcard := "*." + args.BaseDomain
	cert, err := acm.NewCertificate(ctx, name+"-cert", &acm.CertificateArgs{
		DomainName:       pulumi.String(args.BaseDomain),
		ValidationMethod: pulumi.String("DNS"),
		SubjectAlternativeNames: pulumi.StringArray{
			pulumi.String(wildcard),
		},
		Tags: pulumi.StringMap{
			"ajent-capability": pulumi.String("infrastructure.tenant-dns-tls"),
		},
	}, pulumi.Parent(component))
	if err != nil {
		return nil, fmt.Errorf("acm certificate: %w", err)
	}

	dvo := cert.DomainValidationOptions.Index(pulumi.Int(0))
	validationRecord, err := route53.NewRecord(ctx, name+"-acm-validation", &route53.RecordArgs{
		ZoneId: pulumi.String(args.HostedZoneID),
		Name:   dvo.ResourceRecordName().Elem(),
		Type:   dvo.ResourceRecordType().Elem(),
		Records: pulumi.StringArray{
			dvo.ResourceRecordValue().Elem(),
		},
		Ttl:            pulumi.Int(60),
		AllowOverwrite: pulumi.Bool(true),
	}, pulumi.Parent(component))
	if err != nil {
		return nil, fmt.Errorf("acm validation DNS record: %w", err)
	}

	validation, err := acm.NewCertificateValidation(ctx, name+"-cert-validation", &acm.CertificateValidationArgs{
		CertificateArn: cert.Arn,
		ValidationRecordFqdns: pulumi.StringArray{
			validationRecord.Fqdn,
		},
	}, pulumi.Parent(component))
	if err != nil {
		return nil, fmt.Errorf("certificate validation resource: %w", err)
	}

	fqdn := args.BaseDomain
	if args.Slug != "" {
		fqdn = args.Slug + "." + args.BaseDomain
		if args.LoadBalancerDNS == "" || args.LoadBalancerZoneID == "" {
			return nil, errors.New("LoadBalancerDNS and LoadBalancerZoneID required when Slug is set")
		}
		_, err := route53.NewRecord(ctx, name+"-slug", &route53.RecordArgs{
			ZoneId: pulumi.String(args.HostedZoneID),
			Name:   pulumi.String(fqdn),
			Type:   pulumi.String("A"),
			Aliases: route53.RecordAliasArray{
				&route53.RecordAliasArgs{
					Name:                 pulumi.String(args.LoadBalancerDNS),
					ZoneId:               pulumi.String(args.LoadBalancerZoneID),
					EvaluateTargetHealth: pulumi.Bool(true),
				},
			},
		}, pulumi.Parent(component))
		if err != nil {
			return nil, fmt.Errorf("slug DNS record: %w", err)
		}
	}

	component.CertificateARN = validation.CertificateArn
	component.FQDN = pulumi.String(fqdn).ToStringOutput()
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"certificateArn": component.CertificateARN,
		"fqdn":           component.FQDN,
	}); err != nil {
		return nil, err
	}
	return component, nil
}

// ValidateArgs exports argument checks for unit tests without a Pulumi context.
func ValidateArgs(a Args) error { return validateArgs(a) }

func validateArgs(a Args) error {
	if strings.TrimSpace(a.Name) == "" {
		return errors.New("Name required")
	}
	if !domainRE.MatchString(a.BaseDomain) || strings.Contains(a.BaseDomain, "*") {
		return errors.New("BaseDomain must be a bare domain without wildcard")
	}
	if !zoneRE.MatchString(a.HostedZoneID) {
		return errors.New("HostedZoneID required")
	}
	if a.Slug != "" && !slugRE.MatchString(a.Slug) {
		return errors.New("invalid Slug")
	}
	return nil
}

// ValidateWildcardSAN ensures cert request includes *.base.
func ValidateWildcardSAN(base, san string) error {
	if san != "*."+base {
		return fmt.Errorf("SAN must be *.%s", base)
	}
	return nil
}
