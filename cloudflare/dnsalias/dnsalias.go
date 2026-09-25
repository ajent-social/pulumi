// Package dnsalias creates DNS-only Cloudflare records for service aliases.
package dnsalias

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/pulumi/pulumi-cloudflare/sdk/v5/go/cloudflare"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

var (
	zoneRE = regexp.MustCompile(`^[a-f0-9]{32}$`)
	nameRE = regexp.MustCompile(`^[a-zA-Z0-9._*-]+$`)
)

// RecordSpec describes one DNS-only record. Proxied is always false.
type RecordSpec struct {
	Name    string               // relative label or FQDN accepted by Cloudflare
	Type    string               // CNAME, A, or AAAA
	Content pulumi.StringInput   // target (e.g. ALB DNS name)
	TTL     int                  // default 60; ignored by CF when proxied (we never proxy)
	Comment string
}

// Args configures DNS-only alias records in an existing zone.
type Args struct {
	ZoneID  string
	Records []RecordSpec
}

// Bundle holds created record hostnames keyed by Name.
type Bundle struct {
	pulumi.ResourceState

	Hostnames pulumi.StringMapOutput `pulumi:"hostnames"`
}

// New creates DNS-only (Proxied=false) Cloudflare records.
// Callers own the zone and API token (cloudflare:apiToken provider config).
func New(ctx *pulumi.Context, name string, args Args, opts ...pulumi.ResourceOption) (*Bundle, error) {
	if ctx == nil {
		return nil, errors.New("Pulumi context is required")
	}
	if err := validateArgs(args); err != nil {
		return nil, err
	}
	component := &Bundle{}
	if err := ctx.RegisterComponentResource("ajent:cloudflare:dnsalias:Bundle", name, component, opts...); err != nil {
		return nil, err
	}
	tagsComment := "AMSL dnsalias; DNS-only (proxied=false)"

	hostMap := pulumi.StringMap{}
	seen := map[string]struct{}{}
	for i, spec := range args.Records {
		ttl := spec.TTL
		if ttl == 0 {
			ttl = 60
		}
		comment := spec.Comment
		if comment == "" {
			comment = tagsComment
		}
		logical := fmt.Sprintf("%s-%d-%s", name, i, sanitize(spec.Name))
		if _, ok := seen[logical]; ok {
			return nil, fmt.Errorf("record logical name collision %q", logical)
		}
		seen[logical] = struct{}{}
		rec, err := cloudflare.NewRecord(ctx, logical, &cloudflare.RecordArgs{
			ZoneId:  pulumi.String(args.ZoneID),
			Name:    pulumi.String(spec.Name),
			Type:    pulumi.String(strings.ToUpper(spec.Type)),
			Content: spec.Content,
			Ttl:     pulumi.IntPtr(ttl),
			Proxied: pulumi.BoolPtr(false),
			Comment: pulumi.StringPtr(comment),
		}, pulumi.Parent(component))
		if err != nil {
			return nil, fmt.Errorf("record %q: %w", spec.Name, err)
		}
		hostMap[spec.Name] = rec.Hostname
	}

	component.Hostnames = hostMap.ToStringMapOutput()
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"hostnames": component.Hostnames,
	}); err != nil {
		return nil, err
	}
	return component, nil
}

// ValidationCNAMEArgs creates a DNS-only CNAME for ACM (or similar) DNS validation.
// Proxied must stay false or validation never completes.
type ValidationCNAMEArgs struct {
	ZoneID  string
	Name    pulumi.StringInput // FQDN without trailing dot preferred
	Content pulumi.StringInput
	Comment string
}

// ValidationRecord is a single ACM validation CNAME.
type ValidationRecord struct {
	pulumi.ResourceState

	Hostname pulumi.StringOutput `pulumi:"hostname"`
}

// NewValidationCNAME creates one DNS-only validation CNAME.
func NewValidationCNAME(ctx *pulumi.Context, name string, args ValidationCNAMEArgs, opts ...pulumi.ResourceOption) (*ValidationRecord, error) {
	if ctx == nil {
		return nil, errors.New("Pulumi context is required")
	}
	if !zoneRE.MatchString(args.ZoneID) {
		return nil, errors.New("ZoneID must be a 32-char hex Cloudflare zone id")
	}
	if args.Name == nil || args.Content == nil {
		return nil, errors.New("Name and Content are required")
	}
	component := &ValidationRecord{}
	if err := ctx.RegisterComponentResource("ajent:cloudflare:dnsalias:ValidationCNAME", name, component, opts...); err != nil {
		return nil, err
	}
	comment := args.Comment
	if comment == "" {
		comment = "AMSL dnsalias ACM validation; DNS-only (proxied=false)"
	}
	rec, err := cloudflare.NewRecord(ctx, name+"-acm", &cloudflare.RecordArgs{
		ZoneId:  pulumi.String(args.ZoneID),
		Name:    args.Name,
		Type:    pulumi.String("CNAME"),
		Content: args.Content,
		Ttl:     pulumi.IntPtr(60),
		Proxied: pulumi.BoolPtr(false),
		Comment: pulumi.StringPtr(comment),
	}, pulumi.Parent(component))
	if err != nil {
		return nil, fmt.Errorf("validation cname: %w", err)
	}
	component.Hostname = rec.Hostname
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"hostname": component.Hostname,
	}); err != nil {
		return nil, err
	}
	return component, nil
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '-'
		}
	}, s)
}

// ValidateArgs exports argument checks for unit tests without a Pulumi context.
func ValidateArgs(a Args) error { return validateArgs(a) }

// RejectProxied documents that Proxied=true is never accepted.
func RejectProxied(proxied bool) error {
	if proxied {
		return errors.New("Proxied=true is rejected; AMSL dnsalias is DNS-only so origin TLS and SG policy remain authoritative")
	}
	return nil
}

func validateArgs(a Args) error {
	if !zoneRE.MatchString(a.ZoneID) {
		return errors.New("ZoneID must be a 32-char hex Cloudflare zone id")
	}
	if len(a.Records) == 0 {
		return errors.New("Records must not be empty")
	}
	seen := map[string]struct{}{}
	for _, r := range a.Records {
		if r.Name == "" || !nameRE.MatchString(r.Name) {
			return fmt.Errorf("invalid record Name %q", r.Name)
		}
		if _, ok := seen[r.Name]; ok {
			return fmt.Errorf("duplicate record Name %q", r.Name)
		}
		seen[r.Name] = struct{}{}
		switch strings.ToUpper(r.Type) {
		case "CNAME", "A", "AAAA":
		default:
			return fmt.Errorf("record %q Type must be CNAME, A, or AAAA", r.Name)
		}
		if r.Content == nil {
			return fmt.Errorf("record %q Content is required", r.Name)
		}
		if r.TTL < 0 {
			return fmt.Errorf("record %q TTL must be >= 0", r.Name)
		}
	}
	return nil
}
