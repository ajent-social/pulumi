// Package appsecrets creates Secrets Manager secret shells without plaintext.
package appsecrets

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/secretsmanager"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

var (
	nameRE   = regexp.MustCompile(`^[a-zA-Z0-9/_+=.@-]+$`)
	prefixRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}[a-z0-9]$`)
)

// SecretSpec names one secret under Prefix.
type SecretSpec struct {
	Name string // relative name; full name is Prefix/Name
}

// Args configures secret shells. Plaintext values are never accepted.
type Args struct {
	Prefix                     string
	Secrets                    []SecretSpec
	KMSKeyID                   string // optional CMK
	RecoveryWindowInDays       int    // default 30; min 7 unless ForceDeleteWithoutRecovery
	ForceDeleteWithoutRecovery bool
}

// Bundle holds created secret ARNs keyed by relative name.
type Bundle struct {
	pulumi.ResourceState

	ARNs pulumi.StringMapOutput `pulumi:"arns"`
}

// New creates empty secret resources (no SecretVersion).
func New(ctx *pulumi.Context, name string, args Args, opts ...pulumi.ResourceOption) (*Bundle, error) {
	if ctx == nil {
		return nil, errors.New("Pulumi context is required")
	}
	if err := validateArgs(args); err != nil {
		return nil, err
	}
	component := &Bundle{}
	if err := ctx.RegisterComponentResource("ajent:aws:appsecrets:Bundle", name, component, opts...); err != nil {
		return nil, err
	}
	tags := pulumi.StringMap{"ajent-capability": pulumi.String("infrastructure.app-secrets")}
	recovery := args.RecoveryWindowInDays
	if recovery == 0 && !args.ForceDeleteWithoutRecovery {
		recovery = 30
	}

	arnMap := pulumi.StringMap{}
	seenLogical := map[string]struct{}{}
	for i, spec := range args.Secrets {
		full := args.Prefix + "/" + spec.Name
		sArgs := &secretsmanager.SecretArgs{
			Name:        pulumi.String(full),
			Description: pulumi.String("AMSL app secret shell; populate out of band"),
			Tags:        tags,
		}
		if args.KMSKeyID != "" {
			sArgs.KmsKeyId = pulumi.String(args.KMSKeyID)
		}
		if args.ForceDeleteWithoutRecovery {
			// RecoveryWindowInDays=0 schedules immediate deletion on destroy.
			sArgs.RecoveryWindowInDays = pulumi.Int(0)
		} else {
			sArgs.RecoveryWindowInDays = pulumi.Int(recovery)
		}
		logical := fmt.Sprintf("%s-%d-%s", name, i, sanitize(spec.Name))
		if _, ok := seenLogical[logical]; ok {
			return nil, fmt.Errorf("secret logical name collision %q", logical)
		}
		seenLogical[logical] = struct{}{}
		sec, err := secretsmanager.NewSecret(ctx, logical, sArgs, pulumi.Parent(component))
		if err != nil {
			return nil, fmt.Errorf("secret %q: %w", full, err)
		}
		arnMap[spec.Name] = sec.Arn
	}

	component.ARNs = arnMap.ToStringMapOutput()
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"arns": component.ARNs,
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

func validateArgs(a Args) error {
	if !prefixRE.MatchString(a.Prefix) {
		return errors.New("Prefix must be a short lowercase identifier")
	}
	if len(a.Secrets) == 0 {
		return errors.New("Secrets must not be empty")
	}
	seen := map[string]struct{}{}
	for _, s := range a.Secrets {
		if s.Name == "" || !nameRE.MatchString(s.Name) {
			return fmt.Errorf("invalid secret name %q", s.Name)
		}
		if strings.Contains(s.Name, "/") {
			return fmt.Errorf("secret name %q must not contain / (Prefix owns the path)", s.Name)
		}
		if _, ok := seen[s.Name]; ok {
			return fmt.Errorf("duplicate secret name %q", s.Name)
		}
		seen[s.Name] = struct{}{}
	}
	if a.ForceDeleteWithoutRecovery {
		if a.RecoveryWindowInDays != 0 {
			return errors.New("RecoveryWindowInDays must be 0 when ForceDeleteWithoutRecovery is set")
		}
	} else if a.RecoveryWindowInDays != 0 && a.RecoveryWindowInDays < 7 {
		return errors.New("RecoveryWindowInDays must be >= 7 unless ForceDeleteWithoutRecovery")
	}
	return nil
}

// RejectPlaintext is a documentation/test helper: AMSL never accepts secret values.
func RejectPlaintext(hasPlaintextField bool) error {
	if hasPlaintextField {
		return errors.New("plaintext secret values are rejected; populate versions out of band")
	}
	return nil
}
