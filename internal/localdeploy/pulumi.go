package localdeploy

import (
	"context"
	"io"

	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optpreview"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optup"
)

// PulumiStack implements Stack with the Automation API over an existing
// local project directory; it runs the project's own program and backend.
type PulumiStack struct {
	stack auto.Stack
}

// SelectPulumiStack selects an existing stack; it never creates one.
func SelectPulumiStack(ctx context.Context, workDir, stack string) (*PulumiStack, error) {
	s, err := auto.SelectStackLocalSource(ctx, stack, workDir)
	if err != nil {
		return nil, err
	}
	return &PulumiStack{stack: s}, nil
}

// Config implements Stack. Keys are fully qualified (namespace:key).
func (p *PulumiStack) Config(ctx context.Context) (map[string]ConfigValue, error) {
	all, err := p.stack.GetAllConfig(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]ConfigValue, len(all))
	for k, v := range all {
		out[k] = ConfigValue{Value: v.Value, Secret: v.Secret}
	}
	return out, nil
}

// SetConfig implements Stack.
func (p *PulumiStack) SetConfig(ctx context.Context, key string, v ConfigValue) error {
	return p.stack.SetConfig(ctx, key, auto.ConfigValue{Value: v.Value, Secret: v.Secret})
}

// RemoveConfig implements Stack.
func (p *PulumiStack) RemoveConfig(ctx context.Context, key string) error {
	return p.stack.RemoveConfig(ctx, key)
}

// Preview implements Stack, writing the full diff to out.
func (p *PulumiStack) Preview(ctx context.Context, out io.Writer) (map[string]int, error) {
	res, err := p.stack.Preview(ctx, optpreview.Diff(), optpreview.ProgressStreams(out))
	if err != nil {
		return nil, err
	}
	changes := make(map[string]int, len(res.ChangeSummary))
	for op, n := range res.ChangeSummary {
		changes[string(op)] = n
	}
	return changes, nil
}

// Up implements Stack.
func (p *PulumiStack) Up(ctx context.Context, out io.Writer) error {
	_, err := p.stack.Up(ctx, optup.ProgressStreams(out))
	return err
}
