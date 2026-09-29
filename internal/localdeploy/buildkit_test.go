package localdeploy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testToken = "not-a-real-registry-password"

func TestBuildctlArgs(t *testing.T) {
	got := strings.Join(buildctlArgs(BuildRequest{
		Context: "/src", Dockerfile: "/src/build/Dockerfile.app", Target: "runtime",
		Platforms: []string{"linux/amd64", "linux/arm64"},
		BuildArgs: map[string]string{"B": "2", "A": "1"},
		Labels:    map[string]string{"l": "v"},
		Tag:       testRepo + ":t",
	}, "tcp://builder.example:1234", &TLSFiles{CACert: "/ca", Cert: "/c", Key: "/k", ServerName: "builder"}, "/tmp/m.json"), " ")
	want := "buildctl --addr tcp://builder.example:1234 --tlscacert /ca --tlscert /c --tlskey /k --tlsservername builder" +
		" build --frontend dockerfile.v0 --local context=/src --local dockerfile=/src/build" +
		" --opt filename=Dockerfile.app --opt platform=linux/amd64,linux/arm64 --opt target=runtime" +
		" --opt build-arg:A=1 --opt build-arg:B=2 --opt label:l=v" +
		" --output type=image,name=" + testRepo + ":t,push=true --metadata-file /tmp/m.json"
	if got != want {
		t.Fatalf("buildctlArgs =\n%s\nwant\n%s", got, want)
	}
	noFile := strings.Join(buildctlArgs(BuildRequest{Context: "/src", Platforms: []string{"linux/arm64"}, Tag: "r:t"}, "unix:///s", nil, "/m"), " ")
	if !strings.Contains(noFile, "--local dockerfile=/src --opt filename=Dockerfile") || strings.Contains(noFile, "--tls") {
		t.Fatalf("buildctlArgs without dockerfile or tls = %s", noFile)
	}
}

// buildctlFake checks what a real buildctl would receive and writes the
// metadata file it would write.
type buildctlFake struct {
	t         *testing.T
	configDir string
	auth      string
	perm      os.FileMode
}

func (f *buildctlFake) Run(_ context.Context, _ string, argv, env []string, _ io.Writer) error {
	for _, a := range argv {
		if strings.Contains(a, testToken) {
			f.t.Fatalf("registry password in argv: %v", argv)
		}
	}
	if len(env) != 1 || !strings.HasPrefix(env[0], "DOCKER_CONFIG=") {
		f.t.Fatalf("env = %v, want only DOCKER_CONFIG", env)
	}
	f.configDir = strings.TrimPrefix(env[0], "DOCKER_CONFIG=")
	p := filepath.Join(f.configDir, "config.json")
	st, err := os.Stat(p)
	if err != nil {
		f.t.Fatal(err)
	}
	f.perm = st.Mode().Perm()
	var cfg struct {
		Auths map[string]struct{ Auth string } `json:"auths"`
	}
	b, _ := os.ReadFile(p)
	if err := json.Unmarshal(b, &cfg); err != nil {
		f.t.Fatal(err)
	}
	f.auth = cfg.Auths[testAccount+".dkr.ecr.us-east-1.amazonaws.com"].Auth
	meta := argv[len(argv)-1]
	return os.WriteFile(meta, []byte(`{"containerimage.digest":"`+newDigest+`"}`), 0o600)
}

func TestBuildctlBuildPush(t *testing.T) {
	fake := &buildctlFake{t: t}
	b := &Buildctl{Commander: fake, Addr: "unix:///s"}
	if _, err := b.BuildPush(context.Background(), BuildRequest{}, io.Discard); err == nil {
		t.Fatal("BuildPush before Login succeeded")
	}
	auth := RegistryAuth{Endpoint: "https://" + testAccount + ".dkr.ecr.us-east-1.amazonaws.com", Username: "AWS", Password: testToken}
	if err := b.Login(context.Background(), auth); err != nil {
		t.Fatal(err)
	}
	digest, err := b.BuildPush(context.Background(), BuildRequest{Context: "/src", Platforms: []string{"linux/arm64"}, Tag: testRepo + ":t"}, io.Discard)
	if err != nil || digest != newDigest {
		t.Fatalf("BuildPush = %q, %v", digest, err)
	}
	if want := base64.StdEncoding.EncodeToString([]byte("AWS:" + testToken)); fake.auth != want {
		t.Fatalf("docker config auth = %q, want the ECR credentials keyed by registry host", fake.auth)
	}
	if fake.perm != 0o600 {
		t.Fatalf("docker config mode = %v, want 0600", fake.perm)
	}
	if _, err := os.Stat(fake.configDir); !os.IsNotExist(err) {
		t.Fatalf("temporary docker config survived the build: %v", err)
	}
}

func TestReadMetadataDigest(t *testing.T) {
	dir := t.TempDir()
	for body, ok := range map[string]bool{
		`{"containerimage.digest":"` + newDigest + `"}`: true,
		`{}`:       false,
		`not json`: false,
	} {
		p := filepath.Join(dir, "m.json")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readMetadataDigest(p); (err == nil) != ok {
			t.Errorf("readMetadataDigest(%s) err = %v, want ok=%t", body, err, ok)
		}
	}
}
