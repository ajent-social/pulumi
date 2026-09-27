package localdeploy

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitRepo creates a bare origin and a clone holding a committed Pulumi
// project in infra/, isolated from the operator's git configuration.
func gitRepo(t *testing.T) (clone, origin string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for k, v := range map[string]string{
		"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@example.com",
		"GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@example.com",
	} {
		t.Setenv(k, v)
	}
	root := t.TempDir()
	origin, clone = filepath.Join(root, "origin.git"), filepath.Join(root, "clone")
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(root, "init", "-q", "--bare", "-b", "main", origin)
	run(root, "clone", "-q", origin, clone)
	if err := os.MkdirAll(filepath.Join(clone, "infra"), 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(clone, "infra", "Pulumi.dev.yaml"), "config:\n  app:image: "+prevRef+"\n")
	writeFile(t, filepath.Join(clone, "README.md"), "x\n")
	run(clone, "add", ".")
	run(clone, "commit", "-q", "-m", "init")
	run(clone, "push", "-q", "origin", "main")
	run(clone, "remote", "set-head", "origin", "--auto")
	return clone, origin
}

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitOut(context.Background(), dir, nil, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestGitPinPRPublishes(t *testing.T) {
	clone, origin := gitRepo(t)
	var gotBase, gotHead, gotBody string
	g := &GitPinPR{Dir: filepath.Join(clone, "infra"), Stack: "acme/app/dev",
		CreatePR: func(_ context.Context, _, base, head, _, body string) (string, error) {
			gotBase, gotHead, gotBody = base, head, body
			return "https://example.com/pr/1", nil
		}}
	ctx := context.Background()
	if err := g.Check(ctx); err != nil {
		t.Fatalf("Check: %v", err)
	}
	before := git(t, clone, "rev-parse", "HEAD")
	// The deploy's SetConfig rewrites the settings file; an unrelated file
	// is also dirty and must stay out of the commit.
	writeFile(t, filepath.Join(clone, "infra", "Pulumi.dev.yaml"), "config:\n  app:image: "+newRef+"\n")
	writeFile(t, filepath.Join(clone, "README.md"), "local edit\n")

	branch, url, err := g.Publish(ctx, PinChange{RunID: "20260926T120000Z-0123456789ab", SourceSHA: testSHA, Stack: "acme/app/dev",
		Images: []ImageResult{{ConfigKey: pinKey, Ref: newRef}}})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if branch != "amsl-deploy/20260926T120000Z-0123456789ab" || url != "https://example.com/pr/1" || gotBase != "main" || gotHead != branch {
		t.Fatalf("branch=%q url=%q base=%q head=%q", branch, url, gotBase, gotHead)
	}
	if !strings.Contains(gotBody, pinKey+": "+newRef) || !strings.Contains(gotBody, testSHA) {
		t.Fatalf("PR body = %q", gotBody)
	}
	tip := git(t, origin, "rev-parse", "refs/heads/"+branch)
	if parent := git(t, origin, "rev-parse", tip+"^"); parent != before {
		t.Fatalf("pin commit parent = %s, want HEAD %s", parent, before)
	}
	if files := git(t, origin, "diff", "--name-only", before, tip); files != "infra/Pulumi.dev.yaml" {
		t.Fatalf("pin commit touches %q, want only the stack settings file", files)
	}
	if got := git(t, origin, "show", tip+":infra/Pulumi.dev.yaml"); !strings.Contains(got, newRef) {
		t.Fatalf("committed settings file = %q", got)
	}
	if git(t, clone, "rev-parse", "HEAD") != before || git(t, clone, "branch", "--show-current") != "main" {
		t.Fatal("Publish moved the operator's checkout")
	}
	if git(t, origin, "rev-parse", "refs/heads/main") != before {
		t.Fatal("Publish pushed to the default branch")
	}
	if staged, unstaged := git(t, clone, "diff", "--cached", "--name-only"), git(t, clone, "diff", "--name-only"); staged != "infra/Pulumi.dev.yaml" || unstaged != "README.md" {
		t.Fatalf("staged %q, unstaged %q; want settings staged and README untouched", staged, unstaged)
	}
}

func TestGitPinPRRefuses(t *testing.T) {
	ctx := context.Background()
	t.Run("modified settings file", func(t *testing.T) {
		clone, _ := gitRepo(t)
		writeFile(t, filepath.Join(clone, "infra", "Pulumi.dev.yaml"), "config: {}\n")
		g := &GitPinPR{Dir: filepath.Join(clone, "infra"), Stack: "dev"}
		if err := g.Check(ctx); err == nil || !strings.Contains(err.Error(), "uncommitted") {
			t.Fatalf("Check = %v", err)
		}
	})
	t.Run("head not on default branch", func(t *testing.T) {
		clone, _ := gitRepo(t)
		writeFile(t, filepath.Join(clone, "README.md"), "y\n")
		git(t, clone, "commit", "-q", "-am", "local")
		g := &GitPinPR{Dir: filepath.Join(clone, "infra"), Stack: "dev"}
		if err := g.Check(ctx); err == nil || !strings.Contains(err.Error(), "other commits") {
			t.Fatalf("Check = %v", err)
		}
	})
	t.Run("missing settings file", func(t *testing.T) {
		clone, _ := gitRepo(t)
		g := &GitPinPR{Dir: filepath.Join(clone, "infra"), Stack: "prod"}
		if err := g.Check(ctx); err == nil || !strings.Contains(err.Error(), "Pulumi.prod.yaml") {
			t.Fatalf("Check = %v", err)
		}
	})
	t.Run("existing branch is not overwritten", func(t *testing.T) {
		clone, origin := gitRepo(t)
		git(t, origin, "branch", "amsl-deploy/run1", "main")
		g := &GitPinPR{Dir: filepath.Join(clone, "infra"), Stack: "dev",
			CreatePR: func(context.Context, string, string, string, string, string) (string, error) {
				t.Fatal("opened a PR after a rejected push")
				return "", nil
			}}
		if err := g.Check(ctx); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(clone, "infra", "Pulumi.dev.yaml"), "config:\n  app:image: "+newRef+"\n")
		if _, _, err := g.Publish(ctx, PinChange{RunID: "run1", SourceSHA: testSHA}); err == nil {
			t.Fatal("Publish overwrote an existing branch")
		}
		if git(t, origin, "rev-parse", "refs/heads/amsl-deploy/run1") != git(t, origin, "rev-parse", "main") {
			t.Fatal("existing branch moved")
		}
	})
	t.Run("head moved during deploy", func(t *testing.T) {
		clone, _ := gitRepo(t)
		g := &GitPinPR{Dir: filepath.Join(clone, "infra"), Stack: "dev"}
		if err := g.Check(ctx); err != nil {
			t.Fatal(err)
		}
		git(t, clone, "commit", "-q", "--allow-empty", "-m", "moved")
		if _, _, err := g.Publish(ctx, PinChange{RunID: "r", SourceSHA: testSHA}); err == nil || !strings.Contains(err.Error(), "moved") {
			t.Fatalf("Publish = %v", err)
		}
	})
}
