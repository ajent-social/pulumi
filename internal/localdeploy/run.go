package localdeploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

var (
	digestRE = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	shaRE    = regexp.MustCompile(`^[a-f0-9]{40}([a-f0-9]{24})?$`)
)

// SourceSHAToken in a build_args value is replaced by the source commit SHA.
const SourceSHAToken = "{source_sha}"

// Options are the per-run operator choices.
type Options struct {
	Plan       bool   // preflight and a build-free preview only
	Yes        bool   // skip the interactive confirmation
	AllowDirty bool   // deploy from a tree with uncommitted changes
	ExpectSHA  string // refuse unless HEAD is exactly this commit
}

// Deployer runs one deploy. Every external effect goes through a port so the
// orchestration is testable without Docker, AWS or Pulumi.
type Deployer struct {
	Config    *Config
	Git       Git
	Commander Commander
	Cloud     Cloud
	Builder   Builder
	Stack     Stack
	Services  Services
	HTTP      *http.Client
	Confirmer Confirmer    // nil means no interactive operator is present
	Pins      PinPublisher // nil disables publishing the pin change
	Now       func() time.Time
	Out       io.Writer // operator-facing progress
	Poll      time.Duration

	rec     *Record
	log     io.Writer
	logFile *os.File
}

// refusal marks an error that stopped the run before the stack was applied.
type refusal struct{ error }

// Run executes the deploy and always writes a run record. It returns the
// record, the record path and the error that ended the run, if any.
func (d *Deployer) Run(ctx context.Context, opts Options) (*Record, string, error) {
	cfg := d.Config
	r := &Record{SchemaVersion: RecordSchemaVersion, Mode: "deploy", StartedAt: d.now(), Stack: cfg.Pulumi.Stack}
	if opts.Plan {
		r.Mode = "plan"
	}
	r.AWS.Region, r.AWS.AccountID = cfg.AWS.Region, cfg.AWS.AccountID
	d.rec, d.log = r, d.out()

	runErr := d.run(ctx, opts)
	if d.logFile != nil {
		runErr = errors.Join(runErr, d.logFile.Close())
	}

	r.FinishedAt = d.now()
	if runErr != nil {
		r.Error = runErr.Error()
		var ref refusal
		if r.Outcome == "" || errors.As(runErr, &ref) {
			r.Outcome = OutcomeRefused
		}
	}
	if r.RunID == "" {
		r.RunID = r.StartedAt.UTC().Format("20060102T150405Z")
	}
	path, err := r.write(cfg.recordDir())
	return r, path, errors.Join(runErr, err)
}

func (d *Deployer) run(ctx context.Context, opts Options) error {
	cfg, r := d.Config, d.rec

	// 1. Preflight.
	sha, err := d.Git.Head(ctx, cfg.BaseDir)
	if err != nil {
		return refusal{fmt.Errorf("preflight: read HEAD: %w", err)}
	}
	if !shaRE.MatchString(sha) {
		return refusal{fmt.Errorf("preflight: HEAD %q is not a full commit SHA", sha)}
	}
	r.Source.SHA = sha
	r.RunID = r.StartedAt.UTC().Format("20060102T150405Z") + "-" + sha[:12]
	if err := d.openLog(); err != nil {
		return refusal{err}
	}
	if opts.ExpectSHA != "" && opts.ExpectSHA != sha {
		return refusal{fmt.Errorf("preflight: HEAD is %s, expected %s", sha, opts.ExpectSHA)}
	}
	dirty, err := d.Git.Dirty(ctx, cfg.BaseDir)
	if err != nil {
		return refusal{fmt.Errorf("preflight: read tree status: %w", err)}
	}
	r.Source.Dirty = dirty
	if dirty && !opts.AllowDirty {
		return refusal{errors.New("preflight: working tree has uncommitted changes; commit them or pass --allow-dirty")}
	}
	acct, err := d.Cloud.CallerAccount(ctx)
	if err != nil {
		return refusal{fmt.Errorf("preflight: resolve AWS caller identity: %w", err)}
	}
	if acct != cfg.AWS.AccountID {
		return refusal{fmt.Errorf("preflight: credentials are for account %s, config requires %s", acct, cfg.AWS.AccountID)}
	}
	current, err := d.Stack.Config(ctx)
	if err != nil {
		return refusal{fmt.Errorf("preflight: read stack config: %w", err)}
	}
	if d.Pins != nil {
		if err := d.Pins.Check(ctx); err != nil {
			return refusal{err}
		}
	}
	d.printf("preflight: source %s, account %s, stack %s\n", sha, acct, cfg.Pulumi.Stack)

	if opts.Plan {
		for _, img := range cfg.Images {
			r.Images = append(r.Images, ImageResult{Name: img.Name, ConfigKey: img.ConfigKey, PreviousRef: current[img.ConfigKey].Value})
		}
		changes, err := d.Stack.Preview(ctx, d.log)
		if err != nil {
			return refusal{fmt.Errorf("plan: preview: %w", err)}
		}
		r.Preview = changes
		r.Outcome = OutcomePlanned
		d.printf("plan: preview of the current pins: %s\n", summarize(changes))
		return nil
	}

	// 2. Gate.
	if err := d.gate(ctx, sha); err != nil {
		return refusal{err}
	}

	// 3-4. Build, push and verify each digest.
	auth, err := d.Cloud.RegistryAuth(ctx)
	if err != nil {
		return refusal{fmt.Errorf("registry auth: %w", err)}
	}
	if err := d.Builder.Login(ctx, auth); err != nil {
		return refusal{fmt.Errorf("registry login: %w", err)}
	}
	tag := sha + "-" + r.StartedAt.UTC().Format("20060102T150405Z")
	for _, img := range cfg.Images {
		res, err := d.buildPush(ctx, img, sha, tag)
		if err != nil {
			return refusal{err}
		}
		res.PreviousRef = current[img.ConfigKey].Value
		r.Images = append(r.Images, res)
	}

	// 5. Pin and preview.
	for _, res := range r.Images {
		if err := d.Stack.SetConfig(ctx, res.ConfigKey, ConfigValue{Value: res.Ref}); err != nil {
			return refusal{d.restoreConfigOnly(ctx, current, fmt.Errorf("pin %s: %w", res.ConfigKey, err))}
		}
	}
	changes, err := d.Stack.Preview(ctx, d.log)
	if err != nil {
		return refusal{d.restoreConfigOnly(ctx, current, fmt.Errorf("preview: %w", err))}
	}
	r.Preview = changes
	d.printf("preview: %s\n", summarize(changes))
	ok, how, err := d.confirm(opts)
	if err != nil || !ok {
		if err == nil {
			err = errors.New("operator declined the change")
		}
		return refusal{d.restoreConfigOnly(ctx, current, err)}
	}
	r.Confirmed = how

	// 6. Apply.
	upErr := d.Stack.Up(ctx, d.log)
	r.Apply = &StepResult{Succeeded: upErr == nil, FinishedAt: d.now()}
	if upErr != nil {
		r.Apply.Error = upErr.Error()
		return d.rollback(ctx, current, fmt.Errorf("apply: %w", upErr))
	}

	// 7. Verify.
	if err := d.verify(ctx, sha, true, &r.Verify); err != nil {
		return d.rollback(ctx, current, fmt.Errorf("verify: %w", err))
	}
	r.Outcome = OutcomeSucceeded
	d.printf("deployed %s to stack %s and verified\n", sha, cfg.Pulumi.Stack)
	return d.publishPins(ctx, sha)
}

// publishPins opens the pin pull request. A failure here does not undo the
// verified deploy; it is recorded and returned so the operator publishes the
// change by hand.
func (d *Deployer) publishPins(ctx context.Context, sha string) error {
	if d.Pins == nil {
		return nil
	}
	res := &PinPRResult{}
	d.rec.PinPR = res
	res.Branch, res.URL, res.err = d.Pins.Publish(context.WithoutCancel(ctx), PinChange{
		RunID: d.rec.RunID, SourceSHA: sha, Stack: d.Config.Pulumi.Stack, Images: d.rec.Images,
	})
	res.FinishedAt = d.now()
	if res.err != nil {
		res.Error = res.err.Error()
		d.printf("PINS NOT PUBLISHED: the stack runs the new pins but the repository does not record them: %v\n", res.err)
		return fmt.Errorf("deployed and verified, but publishing the pins failed: %w", res.err)
	}
	d.printf("pins: pull request %s (branch %s)\n", res.URL, res.Branch)
	return nil
}

func (d *Deployer) gate(ctx context.Context, sha string) error {
	g := d.Config.Gate
	res := &GateResult{Command: g.Command, StartedAt: d.now()}
	d.rec.Gate = res
	d.printf("gate: running %s\n", strings.Join(g.Command, " "))
	gctx, cancel := context.WithTimeout(ctx, time.Duration(g.Timeout))
	defer cancel()
	err := d.Commander.Run(gctx, d.Config.path(g.Dir), g.Command, []string{"AMSL_DEPLOY_SOURCE_SHA=" + sha}, d.log)
	res.FinishedAt = d.now()
	if err != nil {
		res.Error = err.Error()
		return fmt.Errorf("gate failed; refusing to deploy: %w", err)
	}
	res.Passed = true
	return nil
}

func (d *Deployer) buildPush(ctx context.Context, img Image, sha, tag string) (ImageResult, error) {
	args := make(map[string]string, len(img.BuildArgs))
	for k, v := range img.BuildArgs {
		args[k] = strings.ReplaceAll(v, SourceSHAToken, sha)
	}
	req := BuildRequest{
		Context:   d.Config.path(img.Context),
		Target:    img.Target,
		Platforms: img.Platforms,
		BuildArgs: args,
		Labels:    map[string]string{"org.opencontainers.image.revision": sha},
		Tag:       img.Repository + ":" + tag,
	}
	if img.Dockerfile != "" {
		req.Dockerfile = d.Config.path(img.Dockerfile)
	}
	d.printf("build: %s for %s\n", img.Name, strings.Join(img.Platforms, ","))
	built, err := d.Builder.BuildPush(ctx, req, d.log)
	if err != nil {
		return ImageResult{}, fmt.Errorf("build %s: %w", img.Name, err)
	}
	if !digestRE.MatchString(built) {
		return ImageResult{}, fmt.Errorf("build %s: builder reported %q, not a sha256 digest", img.Name, built)
	}
	pushed, err := d.Cloud.ImageDigest(ctx, img.Repository, tag)
	if err != nil {
		return ImageResult{}, fmt.Errorf("resolve pushed digest for %s: %w", img.Name, err)
	}
	if pushed != built {
		return ImageResult{}, fmt.Errorf("registry holds %s for %s:%s, builder reported %s", pushed, img.Name, tag, built)
	}
	ref := img.Repository + "@" + built
	d.printf("pushed: %s\n", ref)
	return ImageResult{Name: img.Name, Tag: tag, Digest: built, Ref: ref, ConfigKey: img.ConfigKey}, nil
}

func (d *Deployer) confirm(opts Options) (bool, string, error) {
	if opts.Yes {
		return true, "flag", nil
	}
	if d.Confirmer == nil {
		return false, "", errors.New("no interactive terminal to confirm on; pass --yes to approve non-interactively")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Apply to stack %s?\n", d.Config.Pulumi.Stack)
	for _, img := range d.rec.Images {
		prev := img.PreviousRef
		if prev == "" {
			prev = "(unset)"
		}
		fmt.Fprintf(&b, "  %s: %s -> %s\n", img.ConfigKey, prev, img.Ref)
	}
	fmt.Fprintf(&b, "  changes: %s", summarize(d.rec.Preview))
	ok, err := d.Confirmer.Confirm(b.String())
	return ok, "prompt", err
}

// verify waits for every ECS service and HTTP check within verify.timeout.
// withImages also requires the pinned refs to be running and the JSON body
// checks to hold; rollback verification drops both, since the previous
// release satisfies neither.
func (d *Deployer) verify(ctx context.Context, sha string, withImages bool, out *[]CheckResult) error {
	v := d.Config.Verify
	vctx, cancel := context.WithTimeout(ctx, time.Duration(v.Timeout))
	defer cancel()
	refs := map[string]string{}
	for _, img := range d.rec.Images {
		refs[img.Name] = img.Ref
	}
	var errs []error
	record := func(kind, target string, err error) {
		c := CheckResult{Kind: kind, Target: target, Passed: err == nil, At: d.now()}
		if err != nil {
			c.Detail = err.Error()
			errs = append(errs, fmt.Errorf("%s %s: %w", kind, target, err))
		}
		*out = append(*out, c)
		d.printf("verify: %s %s passed=%t\n", kind, target, err == nil)
	}
	for _, s := range v.ECSServices {
		target := s.Cluster + "/" + s.Service
		deadline, _ := vctx.Deadline()
		err := d.Services.WaitStable(vctx, s.Cluster, s.Service, time.Until(deadline))
		if err == nil && withImages && len(s.Images) > 0 {
			err = d.checkImages(vctx, s, refs)
		}
		record("ecs", target, err)
	}
	for _, h := range v.HTTP {
		if !withImages && h.JSONField != "" {
			continue
		}
		record("http", h.URL, d.pollHTTP(vctx, h, sha))
	}
	return errors.Join(errs...)
}

func (d *Deployer) checkImages(ctx context.Context, s ECSService, refs map[string]string) error {
	running, err := d.Services.PrimaryImages(ctx, s.Cluster, s.Service)
	if err != nil {
		return err
	}
	for _, n := range s.Images {
		if !slices.Contains(running, refs[n]) {
			return fmt.Errorf("primary deployment does not run %s (running %v)", refs[n], running)
		}
	}
	return nil
}

func (d *Deployer) pollHTTP(ctx context.Context, h HTTPCheck, sha string) error {
	for {
		err := d.httpOnce(ctx, h, sha)
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w; last attempt: %v", ctx.Err(), err)
		case <-time.After(d.poll()):
		}
	}
}

func (d *Deployer) httpOnce(ctx context.Context, h HTTPCheck, sha string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.URL, nil)
	if err != nil {
		return err
	}
	resp, err := d.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != h.ExpectStatus {
		return fmt.Errorf("status %d, want %d", resp.StatusCode, h.ExpectStatus)
	}
	if h.JSONField == "" {
		return nil
	}
	want := h.JSONEquals
	if h.JSONEqualsSourceSHA {
		want = sha
	}
	got, err := jsonField(body, h.JSONField)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("%s is %q, want %q", h.JSONField, got, want)
	}
	return nil
}

// jsonField reads a dot-separated path from a JSON object. Strings compare by
// value; other values compare by their JSON encoding.
func jsonField(body []byte, path string) (string, error) {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return "", fmt.Errorf("body is not JSON: %w", err)
	}
	for _, part := range strings.Split(path, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return "", fmt.Errorf("%s: not an object at %q", path, part)
		}
		if v, ok = m[part]; !ok {
			return "", fmt.Errorf("%s: field %q missing", path, part)
		}
	}
	if s, ok := v.(string); ok {
		return s, nil
	}
	b, err := json.Marshal(v)
	return string(b), err
}

// restoreConfigOnly puts the previous pins back when nothing was applied.
func (d *Deployer) restoreConfigOnly(ctx context.Context, prev map[string]ConfigValue, cause error) error {
	if err := d.restorePins(context.WithoutCancel(ctx), prev); err != nil {
		return fmt.Errorf("%w; ALSO FAILED to restore stack config, it still carries the new pins: %v", cause, err)
	}
	return cause
}

func (d *Deployer) restorePins(ctx context.Context, prev map[string]ConfigValue) error {
	var errs []error
	for _, img := range d.Config.Images {
		if v, ok := prev[img.ConfigKey]; ok {
			errs = append(errs, d.Stack.SetConfig(ctx, img.ConfigKey, v))
		} else {
			errs = append(errs, d.Stack.RemoveConfig(ctx, img.ConfigKey))
		}
	}
	return errors.Join(errs...)
}

// rollback restores the previous pins, re-applies them and waits for the ECS
// services. It runs even if the caller's context is cancelled.
func (d *Deployer) rollback(ctx context.Context, prev map[string]ConfigValue, cause error) error {
	ctx = context.WithoutCancel(ctx)
	rb := &RollbackResult{Reason: cause.Error()}
	d.rec.Rollback = rb
	d.printf("ROLLBACK: %v\n", cause)
	err := d.restorePins(ctx, prev)
	if err == nil {
		err = d.Stack.Up(ctx, d.log)
	}
	if err == nil {
		err = d.verify(ctx, "", false, &rb.Checks)
	}
	rb.FinishedAt = d.now()
	if err != nil {
		rb.Error = err.Error()
		d.rec.Outcome = OutcomeRollbackFailed
		d.printf("ROLLBACK FAILED: the stack may be running neither the new nor the previous release: %v\n", err)
		return fmt.Errorf("%w; ROLLBACK FAILED: %v", cause, err)
	}
	rb.Succeeded = true
	d.rec.Outcome = OutcomeRolledBack
	d.printf("rollback: previous pins re-applied\n")
	return fmt.Errorf("%w; rolled back to the previous pins", cause)
}

func (d *Deployer) openLog() error {
	dir := d.Config.recordDir()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create record dir: %w", err)
	}
	name := d.rec.RunID + ".log"
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return fmt.Errorf("open run log: %w", err)
	}
	d.logFile = f
	d.log = io.MultiWriter(f, d.out())
	d.rec.Log = name
	return nil
}

func summarize(changes map[string]int) string {
	if len(changes) == 0 {
		return "no changes"
	}
	parts := make([]string, 0, len(changes))
	for op, n := range changes {
		parts = append(parts, fmt.Sprintf("%s=%d", op, n))
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

func (d *Deployer) printf(format string, a ...any) { _, _ = fmt.Fprintf(d.log, format, a...) }

func (d *Deployer) out() io.Writer {
	if d.Out == nil {
		return io.Discard
	}
	return d.Out
}

func (d *Deployer) now() time.Time {
	if d.Now == nil {
		return time.Now()
	}
	return d.Now()
}

func (d *Deployer) poll() time.Duration {
	if d.Poll <= 0 {
		return 5 * time.Second
	}
	return d.Poll
}
