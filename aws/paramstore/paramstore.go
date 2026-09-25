// Package paramstore creates SSM Parameter Store name shells without plaintext.
package paramstore

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ssm"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

const placeholderValue = "REPLACE_OUT_OF_BAND"

var (
	prefixRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}[a-z0-9]$`)
	nameRE   = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)
)

// ParamSpec names one parameter under Prefix.
type ParamSpec struct {
	Name        string // relative name; full name is /Prefix/Name
	Type        string // "String" (default) or "SecureString"
	Description string
}

// Args configures parameter shells. Plaintext values are never accepted.
type Args struct {
	Prefix   string
	Params   []ParamSpec
	KMSKeyID string // required when any Param is SecureString
}

// Bundle holds created parameter ARNs and names keyed by relative name.
type Bundle struct {
	pulumi.ResourceState

	ARNs  pulumi.StringMapOutput `pulumi:"arns"`
	Names pulumi.StringMapOutput `pulumi:"names"`
}

// New creates SSM parameters with a non-secret placeholder value.
// Callers overwrite values out of band (CLI/CI). Overwrite is false so a
// second apply does not clobber an out-of-band value with the placeholder.
func New(ctx *pulumi.Context, name string, args Args, opts ...pulumi.ResourceOption) (*Bundle, error) {
	if ctx == nil {
		return nil, errors.New("Pulumi context is required")
	}
	if err := validateArgs(args); err != nil {
		return nil, err
	}
	component := &Bundle{}
	if err := ctx.RegisterComponentResource("ajent:aws:paramstore:Bundle", name, component, opts...); err != nil {
		return nil, err
	}
	tags := pulumi.StringMap{
		"ajent-capability":   pulumi.String("infrastructure.param-store"),
		"ajent-populate-oob": pulumi.String("true"),
	}

	arnMap := pulumi.StringMap{}
	nameMap := pulumi.StringMap{}
	seenLogical := map[string]struct{}{}
	for i, spec := range args.Params {
		ptype := spec.Type
		if ptype == "" {
			ptype = "String"
		}
		full := "/" + args.Prefix + "/" + spec.Name
		pArgs := &ssm.ParameterArgs{
			Name:        pulumi.String(full),
			Type:        pulumi.String(ptype),
			Value:       pulumi.String(placeholderValue),
			Overwrite:   pulumi.Bool(false),
			Description: pulumi.String(spec.Description),
			Tags:        tags,
		}
		if ptype == "SecureString" {
			pArgs.KeyId = pulumi.String(args.KMSKeyID)
		}
		logical := fmt.Sprintf("%s-%d-%s", name, i, sanitize(spec.Name))
		if _, ok := seenLogical[logical]; ok {
			return nil, fmt.Errorf("parameter logical name collision %q", logical)
		}
		seenLogical[logical] = struct{}{}
		param, err := ssm.NewParameter(ctx, logical, pArgs, pulumi.Parent(component))
		if err != nil {
			return nil, fmt.Errorf("parameter %q: %w", full, err)
		}
		arnMap[spec.Name] = param.Arn
		nameMap[spec.Name] = param.Name
	}

	component.ARNs = arnMap.ToStringMapOutput()
	component.Names = nameMap.ToStringMapOutput()
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"arns":  component.ARNs,
		"names": component.Names,
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

// RejectPlaintext documents that Values are not part of Args.
func RejectPlaintext(hasValueField bool) error {
	if hasValueField {
		return errors.New("plaintext Values are rejected; populate parameters out of band")
	}
	return nil
}

func validateArgs(a Args) error {
	if !prefixRE.MatchString(a.Prefix) {
		return errors.New("Prefix must be a short lowercase identifier")
	}
	if len(a.Params) == 0 {
		return errors.New("Params must not be empty")
	}
	needKMS := false
	seen := map[string]struct{}{}
	for _, p := range a.Params {
		if p.Name == "" || !nameRE.MatchString(p.Name) {
			return fmt.Errorf("invalid parameter name %q", p.Name)
		}
		if strings.Contains(p.Name, "/") {
			return fmt.Errorf("parameter name %q must not contain /", p.Name)
		}
		if _, ok := seen[p.Name]; ok {
			return fmt.Errorf("duplicate parameter name %q", p.Name)
		}
		seen[p.Name] = struct{}{}
		switch p.Type {
		case "", "String":
		case "SecureString":
			needKMS = true
		default:
			return fmt.Errorf("parameter %q Type must be String or SecureString", p.Name)
		}
	}
	if needKMS && a.KMSKeyID == "" {
		return errors.New("KMSKeyID required when any Param Type is SecureString")
	}
	return nil
}
