package localdeploy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Buildctl builds and pushes on a remote BuildKit daemon with the buildctl
// CLI. buildctl streams the build context from this machine and answers the
// daemon's registry credential requests over the BuildKit session, so the
// registry token never appears in argv, in the daemon's storage or on the
// build host's disk. Locally it lives only in a private temporary Docker
// config for the duration of one build.
type Buildctl struct {
	Commander Commander
	Addr      string
	TLS       *TLSFiles

	auth *RegistryAuth
}

// Login implements Builder; it keeps the credentials in memory.
func (b *Buildctl) Login(_ context.Context, auth RegistryAuth) error {
	if auth.Endpoint == "" || auth.Username == "" || auth.Password == "" {
		return errors.New("incomplete registry credentials")
	}
	b.auth = &auth
	return nil
}

// BuildPush implements Builder.
func (b *Buildctl) BuildPush(ctx context.Context, req BuildRequest, out io.Writer) (string, error) {
	if b.auth == nil {
		return "", errors.New("buildctl: Login was not called")
	}
	tmp, err := os.MkdirTemp("", "amsl-deploy-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := writeDockerConfig(tmp, *b.auth); err != nil {
		return "", err
	}
	meta := filepath.Join(tmp, "metadata.json")
	env := []string{"DOCKER_CONFIG=" + tmp}
	if err := b.Commander.Run(ctx, "", buildctlArgs(req, b.Addr, b.TLS, meta), env, out); err != nil {
		return "", err
	}
	return readMetadataDigest(meta)
}

// writeDockerConfig writes a Docker config holding only auth, readable by
// the current user only.
func writeDockerConfig(dir string, auth RegistryAuth) error {
	host := auth.Endpoint
	if u, err := url.Parse(auth.Endpoint); err == nil && u.Host != "" {
		host = u.Host
	}
	cfg := map[string]any{"auths": map[string]any{
		host: map[string]string{"auth": base64.StdEncoding.EncodeToString([]byte(auth.Username + ":" + auth.Password))},
	}}
	b, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "config.json"), b, 0o600)
}

func buildctlArgs(req BuildRequest, addr string, tls *TLSFiles, metadataFile string) []string {
	args := []string{"buildctl", "--addr", addr}
	if tls != nil {
		args = append(args, "--tlscacert", tls.CACert, "--tlscert", tls.Cert, "--tlskey", tls.Key)
		if tls.ServerName != "" {
			args = append(args, "--tlsservername", tls.ServerName)
		}
	}
	dockerfileDir, filename := req.Context, "Dockerfile"
	if req.Dockerfile != "" {
		dockerfileDir, filename = filepath.Dir(req.Dockerfile), filepath.Base(req.Dockerfile)
	}
	args = append(args, "build",
		"--frontend", "dockerfile.v0",
		"--local", "context="+req.Context,
		"--local", "dockerfile="+dockerfileDir,
		"--opt", "filename="+filename,
		"--opt", "platform="+strings.Join(req.Platforms, ","))
	if req.Target != "" {
		args = append(args, "--opt", "target="+req.Target)
	}
	for _, k := range sortedKeys(req.BuildArgs) {
		args = append(args, "--opt", "build-arg:"+k+"="+req.BuildArgs[k])
	}
	for _, k := range sortedKeys(req.Labels) {
		args = append(args, "--opt", "label:"+k+"="+req.Labels[k])
	}
	return append(args,
		"--output", "type=image,name="+req.Tag+",push=true",
		"--metadata-file", metadataFile)
}

// readMetadataDigest reads the pushed manifest digest from a BuildKit
// metadata file, as written by both buildctl and docker buildx.
func readMetadataDigest(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read build metadata: %w", err)
	}
	var m struct {
		Digest string `json:"containerimage.digest"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", fmt.Errorf("parse build metadata: %w", err)
	}
	if m.Digest == "" {
		return "", errors.New("build metadata has no containerimage.digest")
	}
	return m.Digest, nil
}
