// Package localdeploy drives a digest-pinned deploy from a local machine:
// preflight, gate, build, push, pin, preview, apply, verify and rollback.
package localdeploy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ConfigVersion is the only config schema version this tool accepts.
const ConfigVersion = 1

var (
	regionRE       = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-[0-9]$`)
	accountRE      = regexp.MustCompile(`^[0-9]{12}$`)
	roleARNRE      = regexp.MustCompile(`^arn:aws[a-z-]*:iam::[0-9]{12}:role/[\w+=,.@/-]+$`)
	repoRE         = regexp.MustCompile(`^([0-9]{12})\.dkr\.ecr\.([a-z0-9-]+)\.amazonaws\.com/([a-z0-9]+(?:[._/-][a-z0-9]+)*)$`)
	imageNameRE    = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	platformRE     = regexp.MustCompile(`^[a-z0-9]+/[a-z0-9]+(/[a-z0-9]+)?$`)
	configKeyRE    = regexp.MustCompile(`^[A-Za-z0-9_.-]+:[A-Za-z0-9_.-]+$`)
	buildArgKeyRE  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	branchPrefixRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
	remoteRE       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

// Config is the consumer-owned deploy description. Paths are relative to the
// directory holding the config file.
type Config struct {
	Version   int           `json:"version"`
	AWS       AWSConfig     `json:"aws"`
	Gate      GateConfig    `json:"gate"`
	Images    []Image       `json:"images"`
	Pulumi    PulumiConfig  `json:"pulumi"`
	Verify    VerifyConfig  `json:"verify"`
	Builder   BuilderConfig `json:"builder,omitempty"`
	PinPR     PinPRConfig   `json:"pin_pr,omitempty"`
	RecordDir string        `json:"record_dir,omitempty"`

	// BaseDir is the directory of the config file; set by Load.
	BaseDir string `json:"-"`
}

// AWSConfig names the account the deploy must run in. The caller identity is
// checked against AccountID before anything is pushed.
type AWSConfig struct {
	Region        string `json:"region"`
	AccountID     string `json:"account_id"`
	Profile       string `json:"profile,omitempty"`
	AssumeRoleARN string `json:"assume_role_arn,omitempty"`
}

// GateConfig is the test command that must exit zero before any image is built.
type GateConfig struct {
	Command []string `json:"command"`
	Dir     string   `json:"dir,omitempty"`
	Timeout Duration `json:"timeout"`
}

// Image is one container image built from the source tree and pinned into one
// Pulumi config key as repository@sha256:digest.
type Image struct {
	Name       string            `json:"name"`
	Context    string            `json:"context"`
	Dockerfile string            `json:"dockerfile,omitempty"`
	Target     string            `json:"target,omitempty"`
	Platforms  []string          `json:"platforms"`
	BuildArgs  map[string]string `json:"build_args,omitempty"`
	Repository string            `json:"repository"`
	ConfigKey  string            `json:"config_key"`
}

// Builder kinds.
const (
	BuilderBuildx   = "buildx"   // local docker buildx (the default)
	BuilderBuildkit = "buildkit" // a remote BuildKit daemon driven by buildctl
)

// BuilderConfig selects where images are built. A remote BuildKit daemon
// receives the build context and registry credentials over the BuildKit
// session from this machine; neither is written on the build host.
type BuilderConfig struct {
	Kind string `json:"kind,omitempty"`
	// Addr is the BuildKit daemon address, e.g. tcp://builder.example:1234.
	Addr string    `json:"addr,omitempty"`
	TLS  *TLSFiles `json:"tls,omitempty"`
}

// TLSFiles are the operator's mutual-TLS files for a remote BuildKit daemon.
// They are machine-local, so absolute paths are allowed.
type TLSFiles struct {
	CACert     string `json:"ca_cert"`
	Cert       string `json:"cert"`
	Key        string `json:"key"`
	ServerName string `json:"server_name,omitempty"`
}

// PinPRConfig controls publishing the pin change after a verified deploy:
// a commit on a new branch of the Pulumi project's repository and a pull
// request against its default branch. Enabled unless Enabled is false.
type PinPRConfig struct {
	Enabled      *bool  `json:"enabled,omitempty"`
	BranchPrefix string `json:"branch_prefix,omitempty"`
	Remote       string `json:"remote,omitempty"`
}

// On reports whether the pin pull request is enabled.
func (p PinPRConfig) On() bool { return p.Enabled == nil || *p.Enabled }

// PulumiConfig selects the consumer's Pulumi project directory and stack.
type PulumiConfig struct {
	WorkDir string `json:"work_dir"`
	Stack   string `json:"stack"`
}

// VerifyConfig lists post-apply checks. At least one check is required:
// without one a failed rollout cannot be detected or rolled back.
type VerifyConfig struct {
	Timeout     Duration     `json:"timeout"`
	ECSServices []ECSService `json:"ecs_services,omitempty"`
	HTTP        []HTTPCheck  `json:"http,omitempty"`
}

// ECSService is waited on until steady state. When Images is set, the primary
// deployment's task definition must include each named image's pinned ref.
type ECSService struct {
	Cluster string   `json:"cluster"`
	Service string   `json:"service"`
	Images  []string `json:"images,omitempty"`
}

// HTTPCheck polls URL until it returns ExpectStatus and, optionally, a JSON
// body whose JSONField (dot path) equals JSONEquals or the source SHA.
type HTTPCheck struct {
	URL                 string `json:"url"`
	ExpectStatus        int    `json:"expect_status"`
	JSONField           string `json:"json_field,omitempty"`
	JSONEquals          string `json:"json_equals,omitempty"`
	JSONEqualsSourceSHA bool   `json:"json_equals_source_sha,omitempty"`
}

// Duration is a time.Duration encoded as a Go duration string ("15m").
type Duration time.Duration

// UnmarshalJSON parses a duration string.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("duration must be a string like \"15m\": %w", err)
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

// MarshalJSON renders the duration string.
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }

// Load reads, strictly decodes and validates a config file.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	abs, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	cfg.BaseDir = abs
	return cfg, nil
}

// Parse strictly decodes and validates config bytes. Unknown fields and
// trailing data are rejected.
func Parse(b []byte) (*Config, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return nil, err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing data after config object")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Validate checks every field and cross-field rule; it returns all problems.
func (c *Config) Validate() error {
	var errs []error
	bad := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	if c.Version != ConfigVersion {
		bad("version must be %d", ConfigVersion)
	}
	if !regionRE.MatchString(c.AWS.Region) {
		bad("aws.region %q is not an AWS region", c.AWS.Region)
	}
	if !accountRE.MatchString(c.AWS.AccountID) {
		bad("aws.account_id must be 12 digits")
	}
	if c.AWS.AssumeRoleARN != "" && !roleARNRE.MatchString(c.AWS.AssumeRoleARN) {
		bad("aws.assume_role_arn is not an IAM role ARN")
	}

	if len(c.Gate.Command) == 0 || strings.TrimSpace(c.Gate.Command[0]) == "" {
		bad("gate.command is required")
	}
	if c.Gate.Timeout <= 0 {
		bad("gate.timeout must be positive")
	}
	checkRelPath(&errs, "gate.dir", c.Gate.Dir, true)

	if len(c.Images) == 0 {
		bad("images must list at least one image")
	}
	names, keys := map[string]bool{}, map[string]bool{}
	for i, img := range c.Images {
		p := fmt.Sprintf("images[%d]", i)
		if !imageNameRE.MatchString(img.Name) {
			bad("%s.name %q must be a lowercase DNS label", p, img.Name)
		} else if names[img.Name] {
			bad("%s.name %q is duplicated", p, img.Name)
		}
		names[img.Name] = true
		checkRelPath(&errs, p+".context", img.Context, false)
		checkRelPath(&errs, p+".dockerfile", img.Dockerfile, true)
		if len(img.Platforms) == 0 {
			bad("%s.platforms must list at least one platform", p)
		}
		for _, pl := range img.Platforms {
			if !platformRE.MatchString(pl) {
				bad("%s.platforms entry %q is not os/arch[/variant]", p, pl)
			}
		}
		for k := range img.BuildArgs {
			if !buildArgKeyRE.MatchString(k) {
				bad("%s.build_args key %q is invalid", p, k)
			}
		}
		if m := repoRE.FindStringSubmatch(img.Repository); m == nil {
			bad("%s.repository must be <account>.dkr.ecr.<region>.amazonaws.com/<name> without tag or digest", p)
		} else if m[1] != c.AWS.AccountID || m[2] != c.AWS.Region {
			bad("%s.repository must be in aws.account_id and aws.region", p)
		}
		if !configKeyRE.MatchString(img.ConfigKey) {
			bad("%s.config_key %q must be a fully qualified namespace:key", p, img.ConfigKey)
		} else if keys[img.ConfigKey] {
			bad("%s.config_key %q is duplicated", p, img.ConfigKey)
		}
		keys[img.ConfigKey] = true
	}

	// The Pulumi project may live in a sibling repository, so work_dir may
	// climb out of the config directory; it still may not be absolute.
	if c.Pulumi.WorkDir == "" || filepath.IsAbs(c.Pulumi.WorkDir) {
		bad("pulumi.work_dir must be a relative path")
	}
	if strings.TrimSpace(c.Pulumi.Stack) == "" {
		bad("pulumi.stack is required")
	}

	if c.Verify.Timeout <= 0 {
		bad("verify.timeout must be positive")
	}
	if len(c.Verify.ECSServices) == 0 && len(c.Verify.HTTP) == 0 {
		bad("verify must declare at least one ecs_services or http check")
	}
	for i, s := range c.Verify.ECSServices {
		p := fmt.Sprintf("verify.ecs_services[%d]", i)
		if s.Cluster == "" || s.Service == "" {
			bad("%s needs cluster and service", p)
		}
		for _, n := range s.Images {
			if !names[n] {
				bad("%s.images names unknown image %q", p, n)
			}
		}
	}
	for i, h := range c.Verify.HTTP {
		p := fmt.Sprintf("verify.http[%d]", i)
		u, err := url.Parse(h.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			bad("%s.url must be an absolute http(s) URL", p)
		}
		if h.ExpectStatus < 100 || h.ExpectStatus > 599 {
			bad("%s.expect_status must be an HTTP status code", p)
		}
		wantsJSON := h.JSONEquals != "" || h.JSONEqualsSourceSHA
		if h.JSONEquals != "" && h.JSONEqualsSourceSHA {
			bad("%s: json_equals and json_equals_source_sha are exclusive", p)
		}
		if wantsJSON != (h.JSONField != "") {
			bad("%s: json_field requires exactly one of json_equals or json_equals_source_sha", p)
		}
	}
	checkRelPath(&errs, "record_dir", c.RecordDir, true)

	switch c.Builder.Kind {
	case "", BuilderBuildx:
		if c.Builder.Addr != "" || c.Builder.TLS != nil {
			bad("builder.addr and builder.tls apply only to kind %q", BuilderBuildkit)
		}
	case BuilderBuildkit:
		u, err := url.Parse(c.Builder.Addr)
		switch {
		case err != nil || u.Host == "" && u.Path == "":
			bad("builder.addr must be a BuildKit address such as tcp://host:1234")
		case u.Scheme == "tcp":
			// The daemon runs build steps with host privileges; plaintext TCP
			// would let anyone who can reach the port run them.
			t := c.Builder.TLS
			if t == nil || t.CACert == "" || t.Cert == "" || t.Key == "" {
				bad("builder.tls with ca_cert, cert and key is required for a tcp:// builder")
			}
		case u.Scheme == "unix":
		default:
			bad("builder.addr scheme must be tcp or unix")
		}
	default:
		bad("builder.kind must be %q or %q", BuilderBuildx, BuilderBuildkit)
	}
	if p := c.PinPR.BranchPrefix; p != "" && !branchPrefixRE.MatchString(p) {
		bad("pin_pr.branch_prefix %q is not a safe branch prefix", p)
	}
	if r := c.PinPR.Remote; r != "" && !remoteRE.MatchString(r) {
		bad("pin_pr.remote %q is not a git remote name", r)
	}
	return errors.Join(errs...)
}

// checkRelPath rejects absolute paths and paths escaping the config directory,
// so a checked-in config cannot reach outside its own repository.
func checkRelPath(errs *[]error, field, p string, optional bool) {
	if p == "" {
		if !optional {
			*errs = append(*errs, fmt.Errorf("%s is required", field))
		}
		return
	}
	if filepath.IsAbs(p) || !filepath.IsLocal(filepath.Clean(p)) && filepath.Clean(p) != "." {
		*errs = append(*errs, fmt.Errorf("%s %q must be a relative path inside the config directory", field, p))
	}
}

// path resolves a config-relative path.
func (c *Config) path(p string) string {
	if p == "" {
		return c.BaseDir
	}
	return filepath.Join(c.BaseDir, p)
}

// PulumiDir returns the resolved Pulumi project directory.
func (c *Config) PulumiDir() string { return c.path(c.Pulumi.WorkDir) }

// recordDir returns the resolved run-record directory.
func (c *Config) recordDir() string {
	if c.RecordDir == "" {
		return c.path(".amsl-deploy/runs")
	}
	return c.path(c.RecordDir)
}
