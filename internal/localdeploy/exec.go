package localdeploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// ExecCommander runs commands with os/exec, inheriting the environment.
type ExecCommander struct{}

// Run implements Commander.
func (ExecCommander) Run(ctx context.Context, dir string, argv, env []string, out io.Writer) error {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout, cmd.Stderr = out, out
	return cmd.Run()
}

// GitCLI reads revision state with the git binary.
type GitCLI struct{}

// Head implements Git.
func (GitCLI) Head(ctx context.Context, dir string) (string, error) {
	b, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--verify", "HEAD").Output()
	return strings.TrimSpace(string(b)), err
}

// Dirty implements Git. Untracked files count: they can reach a build context.
func (GitCLI) Dirty(ctx context.Context, dir string) (bool, error) {
	b, err := exec.CommandContext(ctx, "git", "-C", dir, "status", "--porcelain").Output()
	return len(strings.TrimSpace(string(b))) > 0, err
}

// DockerBuildx builds and pushes with `docker buildx build --push`.
type DockerBuildx struct {
	Commander Commander
	// Builder names a buildx builder instance; empty uses the current one.
	Builder string
}

// Login implements Builder. The password goes over stdin, never argv.
func (b DockerBuildx) Login(ctx context.Context, auth RegistryAuth) error {
	cmd := exec.CommandContext(ctx, "docker", "login", "--username", auth.Username, "--password-stdin", auth.Endpoint)
	cmd.Stdin = strings.NewReader(auth.Password)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// BuildPush implements Builder, reading the pushed digest from buildx's
// metadata file.
func (b DockerBuildx) BuildPush(ctx context.Context, req BuildRequest, out io.Writer) (string, error) {
	tmp, err := os.MkdirTemp("", "amsl-deploy-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	meta := filepath.Join(tmp, "metadata.json")
	if err := b.Commander.Run(ctx, "", buildxArgs(req, b.Builder, meta), nil, out); err != nil {
		return "", err
	}
	raw, err := os.ReadFile(meta)
	if err != nil {
		return "", fmt.Errorf("read buildx metadata: %w", err)
	}
	var m struct {
		Digest string `json:"containerimage.digest"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", fmt.Errorf("parse buildx metadata: %w", err)
	}
	if m.Digest == "" {
		return "", errors.New("buildx metadata has no containerimage.digest")
	}
	return m.Digest, nil
}

func buildxArgs(req BuildRequest, builder, metadataFile string) []string {
	args := []string{"docker", "buildx", "build", "--push",
		"--platform", strings.Join(req.Platforms, ","),
		"--tag", req.Tag,
		"--metadata-file", metadataFile}
	if builder != "" {
		args = append(args, "--builder", builder)
	}
	if req.Dockerfile != "" {
		args = append(args, "--file", req.Dockerfile)
	}
	if req.Target != "" {
		args = append(args, "--target", req.Target)
	}
	for _, k := range sortedKeys(req.BuildArgs) {
		args = append(args, "--build-arg", k+"="+req.BuildArgs[k])
	}
	for _, k := range sortedKeys(req.Labels) {
		args = append(args, "--label", k+"="+req.Labels[k])
	}
	return append(args, req.Context)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
