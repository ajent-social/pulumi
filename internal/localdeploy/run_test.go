package localdeploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testSHA    = "0123456789abcdef0123456789abcdef01234567"
	prevRef    = testRepo + "@sha256:1111111111111111111111111111111111111111111111111111111111111111"
	newDigest  = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	newRef     = testRepo + "@" + newDigest
	pinKey     = "app:image"
	otherKey   = "app:replicas"
	otherValue = "2"
)

type fakeGit struct {
	head  string
	dirty bool
}

func (g fakeGit) Head(context.Context, string) (string, error) { return g.head, nil }
func (g fakeGit) Dirty(context.Context, string) (bool, error)  { return g.dirty, nil }

type fakeCommander struct {
	err  error
	argv []string
	env  []string
}

func (c *fakeCommander) Run(_ context.Context, _ string, argv, env []string, out io.Writer) error {
	c.argv, c.env = argv, env
	_, _ = fmt.Fprintln(out, "gate output")
	return c.err
}

type fakeCloud struct {
	account      string
	pushedDigest string
}

func (c fakeCloud) CallerAccount(context.Context) (string, error) { return c.account, nil }
func (c fakeCloud) RegistryAuth(context.Context) (RegistryAuth, error) {
	return RegistryAuth{Endpoint: "https://" + testAccount + ".dkr.ecr.us-east-1.amazonaws.com", Username: "AWS", Password: "p"}, nil
}
func (c fakeCloud) ImageDigest(context.Context, string, string) (string, error) {
	return c.pushedDigest, nil
}

type fakeBuilder struct {
	digest string
	reqs   []BuildRequest
}

func (b *fakeBuilder) Login(context.Context, RegistryAuth) error { return nil }
func (b *fakeBuilder) BuildPush(_ context.Context, req BuildRequest, _ io.Writer) (string, error) {
	b.reqs = append(b.reqs, req)
	return b.digest, nil
}

// fakeStack records config writes and applies. upErrs is consumed per Up.
type fakeStack struct {
	cfg      map[string]ConfigValue
	upErrs   []error
	ups      int
	previews int
	applied  []map[string]ConfigValue // config snapshot at each Up
	setErr   error
}

func (s *fakeStack) Config(context.Context) (map[string]ConfigValue, error) {
	out := map[string]ConfigValue{}
	for k, v := range s.cfg {
		out[k] = v
	}
	return out, nil
}
func (s *fakeStack) SetConfig(_ context.Context, k string, v ConfigValue) error {
	if s.setErr != nil {
		return s.setErr
	}
	s.cfg[k] = v
	return nil
}
func (s *fakeStack) RemoveConfig(_ context.Context, k string) error {
	delete(s.cfg, k)
	return nil
}
func (s *fakeStack) Preview(context.Context, io.Writer) (map[string]int, error) {
	s.previews++
	return map[string]int{"update": 1}, nil
}
func (s *fakeStack) Up(ctx context.Context, _ io.Writer) error {
	snap, _ := s.Config(ctx)
	s.applied = append(s.applied, snap)
	s.ups++
	if len(s.upErrs) > 0 {
		err := s.upErrs[0]
		s.upErrs = s.upErrs[1:]
		return err
	}
	return nil
}

type fakeServices struct {
	stableErr error
	images    []string
	waits     int
}

func (s *fakeServices) WaitStable(context.Context, string, string, time.Duration) error {
	s.waits++
	return s.stableErr
}
func (s *fakeServices) PrimaryImages(context.Context, string, string) ([]string, error) {
	return s.images, nil
}

type fakeConfirmer struct {
	answer bool
	prompt string
}

func (c *fakeConfirmer) Confirm(p string) (bool, error) { c.prompt = p; return c.answer, nil }

type harness struct {
	d        *Deployer
	cmd      *fakeCommander
	builder  *fakeBuilder
	stack    *fakeStack
	services *fakeServices
	version  atomic.Value // what the fake /version endpoint reports
	server   *httptest.Server
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		cmd:      &fakeCommander{},
		builder:  &fakeBuilder{digest: newDigest},
		stack:    &fakeStack{cfg: map[string]ConfigValue{pinKey: {Value: prevRef}, otherKey: {Value: otherValue}}},
		services: &fakeServices{images: []string{newRef, "public.ecr.aws/sidecar@sha256:" + strings.Repeat("3", 64)}},
	}
	h.version.Store(testSHA)
	h.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"build":{"sha":%q}}`, h.version.Load().(string))
	}))
	t.Cleanup(h.server.Close)

	c := validConfig()
	c["verify"].(map[string]any)["http"].([]any)[0].(map[string]any)["url"] = h.server.URL + "/version"
	c["verify"].(map[string]any)["timeout"] = "200ms"
	c["images"].([]any)[0].(map[string]any)["build_args"] = map[string]any{"VERSION": SourceSHAToken}
	cfg, err := Parse(mustJSON(t, c))
	if err != nil {
		t.Fatal(err)
	}
	cfg.BaseDir = t.TempDir()

	clock := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	h.d = &Deployer{
		Config:    cfg,
		Git:       fakeGit{head: testSHA},
		Commander: h.cmd,
		Cloud:     fakeCloud{account: testAccount, pushedDigest: newDigest},
		Builder:   h.builder,
		Stack:     h.stack,
		Services:  h.services,
		HTTP:      h.server.Client(),
		Now:       func() time.Time { clock = clock.Add(time.Second); return clock },
		Poll:      10 * time.Millisecond,
	}
	return h
}

func readRecord(t *testing.T, path string) Record {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var r Record
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestDeploySucceeds(t *testing.T) {
	h := newHarness(t)
	rec, path, err := h.d.Run(context.Background(), Options{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rec.Outcome != OutcomeSucceeded {
		t.Fatalf("outcome = %s", rec.Outcome)
	}
	if got := h.stack.cfg[pinKey].Value; got != newRef {
		t.Fatalf("pin = %q, want %q", got, newRef)
	}
	if h.stack.cfg[otherKey].Value != otherValue {
		t.Fatal("unrelated config key changed")
	}
	if h.stack.ups != 1 {
		t.Fatalf("ups = %d, want 1", h.stack.ups)
	}
	req := h.builder.reqs[0]
	if req.BuildArgs["VERSION"] != testSHA || req.Labels["org.opencontainers.image.revision"] != testSHA {
		t.Fatalf("build request = %+v", req)
	}
	if !strings.HasPrefix(req.Tag, testRepo+":"+testSHA+"-") {
		t.Fatalf("tag = %q, want unique source-SHA tag", req.Tag)
	}
	if strings.Join(h.cmd.env, ",") != "AMSL_DEPLOY_SOURCE_SHA="+testSHA {
		t.Fatalf("gate env = %v", h.cmd.env)
	}

	onDisk := readRecord(t, path)
	if onDisk.Source.SHA != testSHA || onDisk.Outcome != OutcomeSucceeded || onDisk.Confirmed != "flag" {
		t.Fatalf("record = %+v", onDisk)
	}
	if len(onDisk.Images) != 1 || onDisk.Images[0].Ref != newRef || onDisk.Images[0].PreviousRef != prevRef {
		t.Fatalf("record images = %+v", onDisk.Images)
	}
	if onDisk.Gate == nil || !onDisk.Gate.Passed || len(onDisk.Verify) != 2 || onDisk.Rollback != nil {
		t.Fatalf("record gate/verify/rollback = %+v %+v %+v", onDisk.Gate, onDisk.Verify, onDisk.Rollback)
	}
	if !onDisk.FinishedAt.After(onDisk.StartedAt) {
		t.Fatal("record timestamps not ordered")
	}
	log, err := os.ReadFile(filepath.Join(filepath.Dir(path), onDisk.Log))
	if err != nil || !strings.Contains(string(log), "gate output") {
		t.Fatalf("run log missing gate output: %v", err)
	}
}

func TestDeployConfirmsInteractively(t *testing.T) {
	h := newHarness(t)
	conf := &fakeConfirmer{answer: true}
	h.d.Confirmer = conf
	rec, _, err := h.d.Run(context.Background(), Options{})
	if err != nil || rec.Confirmed != "prompt" {
		t.Fatalf("Run = %v, confirmed %q", err, rec.Confirmed)
	}
	if !strings.Contains(conf.prompt, prevRef+" -> "+newRef) {
		t.Fatalf("prompt does not show the pin change:\n%s", conf.prompt)
	}
}

// assertRefusedUnchanged checks that a refusal left the stack as it was.
func assertRefusedUnchanged(t *testing.T, h *harness, rec *Record, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want containing %q", err, want)
	}
	if rec.Outcome != OutcomeRefused {
		t.Fatalf("outcome = %s, want refused", rec.Outcome)
	}
	if h.stack.ups != 0 {
		t.Fatalf("stack applied %d times on a refusal", h.stack.ups)
	}
	if h.stack.cfg[pinKey].Value != prevRef {
		t.Fatalf("pin = %q after refusal, want previous", h.stack.cfg[pinKey].Value)
	}
}

func TestRefusals(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*harness)
		opts    Options
		want    string
		noBuild bool
	}{
		{"dirty tree", func(h *harness) { h.d.Git = fakeGit{head: testSHA, dirty: true} }, Options{Yes: true}, "uncommitted", true},
		{"unexpected commit", func(*harness) {}, Options{Yes: true, ExpectSHA: strings.Repeat("f", 40)}, "expected", true},
		{"short head", func(h *harness) { h.d.Git = fakeGit{head: "abc"} }, Options{Yes: true}, "full commit SHA", true},
		{"wrong account", func(h *harness) { h.d.Cloud = fakeCloud{account: "210987654321", pushedDigest: newDigest} }, Options{Yes: true}, "config requires", true},
		{"gate fails", func(h *harness) { h.cmd.err = errors.New("exit status 1") }, Options{Yes: true}, "gate failed", true},
		{"registry digest differs", func(h *harness) {
			h.d.Cloud = fakeCloud{account: testAccount, pushedDigest: "sha256:" + strings.Repeat("9", 64)}
		}, Options{Yes: true}, "registry holds", false},
		{"builder reports a tag", func(h *harness) { h.builder.digest = "latest" }, Options{Yes: true}, "not a sha256 digest", false},
		{"operator declines", func(h *harness) { h.d.Confirmer = &fakeConfirmer{answer: false} }, Options{}, "declined", false},
		{"no terminal and no --yes", func(*harness) {}, Options{}, "pass --yes", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			tt.setup(h)
			rec, path, err := h.d.Run(context.Background(), tt.opts)
			assertRefusedUnchanged(t, h, rec, err, tt.want)
			if tt.noBuild && len(h.builder.reqs) > 0 {
				t.Fatal("built an image after a pre-build refusal")
			}
			if readRecord(t, path).Outcome != OutcomeRefused {
				t.Fatal("record on disk is not refused")
			}
		})
	}
}

func TestAllowDirtyIsRecorded(t *testing.T) {
	h := newHarness(t)
	h.d.Git = fakeGit{head: testSHA, dirty: true}
	rec, _, err := h.d.Run(context.Background(), Options{Yes: true, AllowDirty: true})
	if err != nil || !rec.Source.Dirty {
		t.Fatalf("Run = %v, dirty recorded = %t", err, rec.Source.Dirty)
	}
}

func TestPlanDoesNotBuildOrPin(t *testing.T) {
	h := newHarness(t)
	rec, _, err := h.d.Run(context.Background(), Options{Plan: true})
	if err != nil || rec.Outcome != OutcomePlanned {
		t.Fatalf("Run = %v, outcome %s", err, rec.Outcome)
	}
	if len(h.builder.reqs) != 0 || h.cmd.argv != nil || h.stack.ups != 0 || h.stack.previews != 1 {
		t.Fatalf("plan built=%d gated=%v ups=%d previews=%d", len(h.builder.reqs), h.cmd.argv, h.stack.ups, h.stack.previews)
	}
	if h.stack.cfg[pinKey].Value != prevRef || rec.Images[0].PreviousRef != prevRef {
		t.Fatal("plan changed or misreported the current pin")
	}
}

func TestVerifyFailureRollsBack(t *testing.T) {
	h := newHarness(t)
	h.version.Store("an-older-build")
	rec, path, err := h.d.Run(context.Background(), Options{Yes: true})
	if err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("error = %v, want rolled back", err)
	}
	if rec.Outcome != OutcomeRolledBack || readRecord(t, path).Outcome != OutcomeRolledBack {
		t.Fatalf("outcome = %s", rec.Outcome)
	}
	if h.stack.ups != 2 {
		t.Fatalf("ups = %d, want apply + rollback", h.stack.ups)
	}
	if got := h.stack.applied[1][pinKey].Value; got != prevRef {
		t.Fatalf("rollback applied pin %q, want previous %q", got, prevRef)
	}
	if !rec.Rollback.Succeeded || len(rec.Rollback.Checks) != 1 || rec.Rollback.Checks[0].Kind != "ecs" {
		t.Fatalf("rollback = %+v", rec.Rollback)
	}
}

func TestWrongRunningImageRollsBack(t *testing.T) {
	h := newHarness(t)
	h.services.images = []string{prevRef}
	rec, _, err := h.d.Run(context.Background(), Options{Yes: true})
	if rec.Outcome != OutcomeRolledBack || !strings.Contains(err.Error(), "does not run") {
		t.Fatalf("outcome = %s, err = %v", rec.Outcome, err)
	}
}

func TestApplyFailureRollsBack(t *testing.T) {
	h := newHarness(t)
	h.stack.upErrs = []error{errors.New("update failed")}
	rec, _, err := h.d.Run(context.Background(), Options{Yes: true})
	if rec.Outcome != OutcomeRolledBack || !strings.Contains(err.Error(), "apply: update failed") {
		t.Fatalf("outcome = %s, err = %v", rec.Outcome, err)
	}
	if rec.Apply.Succeeded || len(rec.Verify) != 0 {
		t.Fatalf("apply = %+v, verify = %+v", rec.Apply, rec.Verify)
	}
}

func TestRollbackFailureIsLoud(t *testing.T) {
	h := newHarness(t)
	h.stack.upErrs = []error{errors.New("update failed"), errors.New("rollback update failed")}
	rec, path, err := h.d.Run(context.Background(), Options{Yes: true})
	if rec.Outcome != OutcomeRollbackFailed || readRecord(t, path).Outcome != OutcomeRollbackFailed {
		t.Fatalf("outcome = %s", rec.Outcome)
	}
	if !strings.Contains(err.Error(), "ROLLBACK FAILED") || rec.Rollback.Succeeded {
		t.Fatalf("err = %v, rollback = %+v", err, rec.Rollback)
	}
}

func TestRollbackUnstableServiceFails(t *testing.T) {
	h := newHarness(t)
	h.services.stableErr = errors.New("exceeded max wait time")
	rec, _, err := h.d.Run(context.Background(), Options{Yes: true})
	if rec.Outcome != OutcomeRollbackFailed || !strings.Contains(err.Error(), "ROLLBACK FAILED") {
		t.Fatalf("outcome = %s, err = %v", rec.Outcome, err)
	}
}

func TestRollbackRemovesPinThatDidNotExist(t *testing.T) {
	h := newHarness(t)
	delete(h.stack.cfg, pinKey)
	h.version.Store("wrong")
	rec, _, _ := h.d.Run(context.Background(), Options{Yes: true})
	if rec.Outcome != OutcomeRolledBack {
		t.Fatalf("outcome = %s", rec.Outcome)
	}
	if _, ok := h.stack.cfg[pinKey]; ok {
		t.Fatal("rollback left a pin that did not exist before the run")
	}
}

func TestRollbackSurvivesCancellation(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	h.stack.upErrs = []error{context.Canceled}
	h.d.Confirmer = confirmThen(cancel)
	rec, _, _ := h.d.Run(ctx, Options{})
	if rec.Outcome != OutcomeRolledBack || h.stack.ups != 2 {
		t.Fatalf("outcome = %s, ups = %d", rec.Outcome, h.stack.ups)
	}
}

// confirmThen approves and then runs f, simulating an interrupt during apply.
type confirmThen func()

func (f confirmThen) Confirm(string) (bool, error) { f(); return true, nil }

func TestJSONField(t *testing.T) {
	body := []byte(`{"a":{"b":"x","n":3,"t":true}}`)
	for path, want := range map[string]string{"a.b": "x", "a.n": "3", "a.t": "true"} {
		if got, err := jsonField(body, path); err != nil || got != want {
			t.Errorf("jsonField(%q) = %q, %v; want %q", path, got, err, want)
		}
	}
	for _, path := range []string{"a.missing", "a.b.c"} {
		if _, err := jsonField(body, path); err == nil {
			t.Errorf("jsonField(%q) succeeded, want error", path)
		}
	}
	if _, err := jsonField([]byte("<html>"), "a"); err == nil {
		t.Error("jsonField accepted a non-JSON body")
	}
}

func TestBuildxArgs(t *testing.T) {
	got := strings.Join(buildxArgs(BuildRequest{
		Context: "/src", Dockerfile: "/src/Dockerfile", Target: "runtime",
		Platforms: []string{"linux/amd64", "linux/arm64"},
		BuildArgs: map[string]string{"B": "2", "A": "1"},
		Labels:    map[string]string{"l": "v"},
		Tag:       testRepo + ":t",
	}, "remote", "/tmp/m.json"), " ")
	want := "docker buildx build --push --platform linux/amd64,linux/arm64 --tag " + testRepo + ":t" +
		" --metadata-file /tmp/m.json --builder remote --file /src/Dockerfile --target runtime" +
		" --build-arg A=1 --build-arg B=2 --label l=v /src"
	if got != want {
		t.Fatalf("buildxArgs =\n%s\nwant\n%s", got, want)
	}
}

type fakePins struct {
	checkErr, publishErr error
	checks, publishes    int
	change               PinChange
}

func (p *fakePins) Check(context.Context) error { p.checks++; return p.checkErr }
func (p *fakePins) Publish(_ context.Context, c PinChange) (string, string, error) {
	p.publishes++
	p.change = c
	if p.publishErr != nil {
		return "amsl-deploy/x", "", p.publishErr
	}
	return "amsl-deploy/x", "https://example.com/pr/1", nil
}

func TestPinsPublishedAfterVerify(t *testing.T) {
	h := newHarness(t)
	pins := &fakePins{}
	h.d.Pins = pins
	rec, path, err := h.d.Run(context.Background(), Options{Yes: true})
	if err != nil || rec.Outcome != OutcomeSucceeded {
		t.Fatalf("Run = %v, outcome %s", err, rec.Outcome)
	}
	if pins.checks != 1 || pins.publishes != 1 || pins.change.SourceSHA != testSHA || pins.change.Images[0].Ref != newRef {
		t.Fatalf("pins = %+v", pins)
	}
	if r := readRecord(t, path); r.PinPR == nil || r.PinPR.URL != "https://example.com/pr/1" || r.PinPR.Error != "" {
		t.Fatalf("record pin_pr = %+v", r.PinPR)
	}
}

func TestPinsCheckRefusesBeforeBuild(t *testing.T) {
	h := newHarness(t)
	h.d.Pins = &fakePins{checkErr: errors.New("pin_pr: HEAD is not on origin/main")}
	rec, _, err := h.d.Run(context.Background(), Options{Yes: true})
	assertRefusedUnchanged(t, h, rec, err, "not on origin/main")
	if len(h.builder.reqs) != 0 || h.cmd.argv != nil {
		t.Fatal("built or gated after a pin_pr preflight refusal")
	}
}

func TestPinsPublishFailureKeepsDeploy(t *testing.T) {
	h := newHarness(t)
	h.d.Pins = &fakePins{publishErr: errors.New("gh: not logged in")}
	rec, path, err := h.d.Run(context.Background(), Options{Yes: true})
	if rec.Outcome != OutcomeSucceeded || err == nil || !strings.Contains(err.Error(), "publishing the pins failed") {
		t.Fatalf("outcome = %s, err = %v", rec.Outcome, err)
	}
	if h.stack.ups != 1 || h.stack.cfg[pinKey].Value != newRef {
		t.Fatal("a pin publishing failure must not roll back a verified deploy")
	}
	if r := readRecord(t, path); r.PinPR == nil || !strings.Contains(r.PinPR.Error, "not logged in") {
		t.Fatalf("record pin_pr = %+v", r.PinPR)
	}
}

func TestPinsNotPublishedOnRollbackOrPlan(t *testing.T) {
	h := newHarness(t)
	pins := &fakePins{}
	h.d.Pins = pins
	h.version.Store("wrong")
	if rec, _, _ := h.d.Run(context.Background(), Options{Yes: true}); rec.Outcome != OutcomeRolledBack || rec.PinPR != nil {
		t.Fatalf("outcome = %s, pin_pr = %+v", rec.Outcome, rec.PinPR)
	}
	h2 := newHarness(t)
	h2.d.Pins = pins
	if rec, _, _ := h2.d.Run(context.Background(), Options{Plan: true}); rec.Outcome != OutcomePlanned {
		t.Fatalf("plan outcome = %s", rec.Outcome)
	}
	if pins.publishes != 0 || pins.checks != 2 {
		t.Fatalf("checks = %d publishes = %d, want 2 and 0", pins.checks, pins.publishes)
	}
}
