// Command amsl-deploy deploys digest-pinned container images from a local
// machine: preflight, gate, build, push, pin, preview, apply, verify, and roll
// back on failure. See contracts/local-deploy.md.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ajent-social/pulumi/internal/localdeploy"
)

// Exit codes. Distinct codes let a wrapper script tell "nothing changed" from
// "the stack needs a human".
const (
	exitOK              = 0
	exitRefused         = 1 // nothing was applied
	exitRolledBack      = 2 // apply or verify failed, previous pins re-applied
	exitRollbackFailed  = 3 // the stack state is unknown
	exitPinsUnpublished = 4 // deployed and verified; the pin pull request failed
	exitUsage           = 64
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("amsl-deploy", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: amsl-deploy [flags] deploy|plan|validate")
		fs.PrintDefaults()
	}
	cfgPath := fs.String("config", "amsl-deploy.json", "deploy config file")
	yes := fs.Bool("yes", false, "apply without the interactive confirmation")
	allowDirty := fs.Bool("allow-dirty", false, "deploy from a working tree with uncommitted changes")
	expectSHA := fs.String("expect-sha", "", "refuse unless HEAD is exactly this commit")
	builder := fs.String("builder", "", "docker buildx builder instance (default: current); buildx builder kind only")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return exitUsage
	}
	verb := fs.Arg(0)

	cfg, err := localdeploy.Load(*cfgPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "invalid config: %v\n", err)
		return exitUsage
	}
	switch verb {
	case "validate":
		_, _ = fmt.Fprintf(stdout, "%s: valid\n", *cfgPath)
		return exitOK
	case "deploy", "plan":
	default:
		fs.Usage()
		return exitUsage
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cloud, err := localdeploy.NewAWS(ctx, cfg.AWS)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "aws: %v\n", err)
		return exitRefused
	}
	stack, err := localdeploy.SelectPulumiStack(ctx, cfg.PulumiDir(), cfg.Pulumi.Stack)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "pulumi: select stack %s: %v\n", cfg.Pulumi.Stack, err)
		return exitRefused
	}
	d := &localdeploy.Deployer{
		Config:    cfg,
		Git:       localdeploy.GitCLI{},
		Commander: localdeploy.ExecCommander{},
		Cloud:     cloud,
		Builder:   newBuilder(cfg.Builder, *builder),
		Stack:     stack,
		Services:  cloud,
		HTTP:      &http.Client{Timeout: 30 * time.Second},
		Out:       stderr,
	}
	if cfg.PinPR.On() {
		d.Pins = &localdeploy.GitPinPR{
			Dir: cfg.PulumiDir(), Stack: cfg.Pulumi.Stack,
			Remote: cfg.PinPR.Remote, BranchPrefix: cfg.PinPR.BranchPrefix,
		}
	}
	if isTerminal(stdin) {
		d.Confirmer = promptConfirmer{in: bufio.NewReader(stdin), out: stderr}
	}
	rec, path, err := d.Run(ctx, localdeploy.Options{
		Plan: verb == "plan", Yes: *yes, AllowDirty: *allowDirty, ExpectSHA: *expectSHA,
	})
	if path != "" {
		_, _ = fmt.Fprintf(stdout, "run record: %s\n", path)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "amsl-deploy: %v\n", err)
	}
	return exitCode(rec, err)
}

func newBuilder(c localdeploy.BuilderConfig, buildxBuilder string) localdeploy.Builder {
	if c.Kind == localdeploy.BuilderBuildkit {
		return &localdeploy.Buildctl{Commander: localdeploy.ExecCommander{}, Addr: c.Addr, TLS: c.TLS}
	}
	return localdeploy.DockerBuildx{Commander: localdeploy.ExecCommander{}, Builder: buildxBuilder}
}

func exitCode(rec *localdeploy.Record, err error) int {
	switch rec.Outcome {
	case localdeploy.OutcomeSucceeded, localdeploy.OutcomePlanned:
		if rec.PinPR != nil && rec.PinPR.Error != "" {
			return exitPinsUnpublished
		}
		if err != nil { // the run passed but its record could not be written
			return exitRefused
		}
		return exitOK
	case localdeploy.OutcomeRolledBack:
		return exitRolledBack
	case localdeploy.OutcomeRollbackFailed:
		return exitRollbackFailed
	}
	return exitRefused
}

// isTerminal reports whether r is an interactive character device.
func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

type promptConfirmer struct {
	in  *bufio.Reader
	out io.Writer
}

// Confirm accepts only a literal "yes".
func (p promptConfirmer) Confirm(prompt string) (bool, error) {
	_, _ = fmt.Fprintf(p.out, "%s\nType yes to apply: ", prompt)
	line, err := p.in.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	return strings.TrimSpace(line) == "yes", nil
}
