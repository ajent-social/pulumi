package localdeploy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// PinChange describes a verified deploy whose pins should be published.
type PinChange struct {
	RunID     string
	SourceSHA string
	Stack     string
	Images    []ImageResult
}

// PinPublisher publishes the stack config change of a verified deploy.
type PinPublisher interface {
	// Check runs in preflight and refuses if Publish could not succeed.
	Check(ctx context.Context) error
	// Publish commits the change on a new branch and opens a pull request.
	Publish(ctx context.Context, c PinChange) (branch, url string, err error)
}

// GitPinPR commits the Pulumi stack settings file on a new branch built with
// git plumbing (the operator's checkout, branch and working tree are left
// alone), pushes it and opens a pull request against the remote's default
// branch. It never pushes to an existing branch.
type GitPinPR struct {
	Dir          string // Pulumi project directory
	Stack        string
	Remote       string // default "origin"
	BranchPrefix string // default "amsl-deploy/"
	// CreatePR opens the pull request and returns its URL; nil uses gh.
	CreatePR func(ctx context.Context, dir, base, head, title, body string) (string, error)

	top, rel, base, head string
}

// Check implements PinPublisher.
func (g *GitPinPR) Check(ctx context.Context) error {
	top, err := gitOut(ctx, g.Dir, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("pin_pr: %s is not in a git repository: %w", g.Dir, err)
	}
	file, err := stackSettingsFile(g.Dir, g.Stack)
	if err != nil {
		return err
	}
	// Resolve symlinks on both sides so the relative path is stable.
	realTop, err1 := filepath.EvalSymlinks(top)
	realFile, err2 := filepath.EvalSymlinks(filepath.Dir(file))
	if err := errors.Join(err1, err2); err != nil {
		return err
	}
	rel, err := filepath.Rel(realTop, filepath.Join(realFile, filepath.Base(file)))
	if err != nil {
		return err
	}
	status, err := gitOut(ctx, top, nil, "status", "--porcelain", "--", rel)
	if err != nil {
		return err
	}
	if status != "" {
		return fmt.Errorf("pin_pr: %s has uncommitted changes (an unmerged pin pull request?); merge or discard them first", rel)
	}
	remote := g.remote()
	def, err := gitOut(ctx, top, nil, "symbolic-ref", "--short", "refs/remotes/"+remote+"/HEAD")
	if err != nil {
		return fmt.Errorf("pin_pr: cannot resolve the default branch of %s (try `git remote set-head %s --auto`): %w", remote, remote, err)
	}
	base := strings.TrimPrefix(def, remote+"/")
	if _, err := gitOut(ctx, top, nil, "fetch", "--quiet", remote, base); err != nil {
		return fmt.Errorf("pin_pr: fetch %s %s: %w", remote, base, err)
	}
	head, err := gitOut(ctx, top, nil, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return err
	}
	if _, err := gitOut(ctx, top, nil, "merge-base", "--is-ancestor", "HEAD", remote+"/"+base); err != nil {
		return fmt.Errorf("pin_pr: HEAD of %s is not on %s/%s, so the pin pull request would carry other commits", top, remote, base)
	}
	g.top, g.rel, g.base, g.head = top, rel, base, head
	return nil
}

// Publish implements PinPublisher.
func (g *GitPinPR) Publish(ctx context.Context, c PinChange) (string, string, error) {
	if g.top == "" {
		return "", "", errors.New("pin_pr: Check was not run")
	}
	head, err := gitOut(ctx, g.top, nil, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", "", err
	}
	if head != g.head {
		return "", "", fmt.Errorf("pin_pr: HEAD of %s moved during the deploy (%s -> %s)", g.top, g.head, head)
	}
	blob, err := gitOut(ctx, g.top, nil, "hash-object", "-w", "--", g.rel)
	if err != nil {
		return "", "", err
	}
	idx, err := os.CreateTemp("", "amsl-deploy-index-")
	if err != nil {
		return "", "", err
	}
	_ = idx.Close()
	defer func() { _ = os.Remove(idx.Name()) }()
	env := []string{"GIT_INDEX_FILE=" + idx.Name()}
	if _, err := gitOut(ctx, g.top, env, "read-tree", g.head); err != nil {
		return "", "", err
	}
	if _, err := gitOut(ctx, g.top, env, "update-index", "--add", "--cacheinfo", "100644,"+blob+","+g.rel); err != nil {
		return "", "", err
	}
	tree, err := gitOut(ctx, g.top, env, "write-tree")
	if err != nil {
		return "", "", err
	}
	title, body := pinMessage(c)
	commit, err := gitOut(ctx, g.top, nil, "commit-tree", tree, "-p", g.head, "-m", title+"\n\n"+body)
	if err != nil {
		return "", "", err
	}
	branch := g.prefix() + c.RunID
	// A plain push would fast-forward an existing branch; an empty lease
	// expectation makes the push fail unless the branch does not exist yet.
	ref := "refs/heads/" + branch
	if _, err := gitOut(ctx, g.top, nil, "push", "--quiet", "--force-with-lease="+ref+":", g.remote(), commit+":"+ref); err != nil {
		return "", "", fmt.Errorf("pin_pr: push %s: %w", branch, err)
	}
	// Stage the file so a fast-forward to the merged pull request applies
	// cleanly over the identical local change.
	if _, err := gitOut(ctx, g.top, nil, "add", "--", g.rel); err != nil {
		return branch, "", fmt.Errorf("pin_pr: stage %s: %w", g.rel, err)
	}
	create := g.CreatePR
	if create == nil {
		create = ghCreatePR
	}
	url, err := create(ctx, g.top, g.base, branch, title, body)
	if err != nil {
		return branch, "", fmt.Errorf("pin_pr: branch %s pushed, but opening the pull request failed: %w", branch, err)
	}
	return branch, url, nil
}

func pinMessage(c PinChange) (title, body string) {
	title = fmt.Sprintf("Pin %s images to %s", c.Stack, short(c.SourceSHA))
	var b strings.Builder
	fmt.Fprintf(&b, "Deployed and verified by amsl-deploy run %s from source %s.\n\n", c.RunID, c.SourceSHA)
	for _, img := range c.Images {
		fmt.Fprintf(&b, "- %s: %s\n", img.ConfigKey, img.Ref)
	}
	return title, b.String()
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// stackSettingsFile returns the stack settings file Pulumi reads for stack,
// which is named after the last segment of a qualified stack name.
func stackSettingsFile(dir, stack string) (string, error) {
	name := stack[strings.LastIndex(stack, "/")+1:]
	for _, ext := range []string{".yaml", ".yml", ".json"} {
		p := filepath.Join(dir, "Pulumi."+name+ext)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("pin_pr: no Pulumi.%s.yaml in %s", name, dir)
}

func (g *GitPinPR) remote() string {
	if g.Remote == "" {
		return "origin"
	}
	return g.Remote
}

func (g *GitPinPR) prefix() string {
	if g.BranchPrefix == "" {
		return "amsl-deploy/"
	}
	return g.BranchPrefix
}

func gitOut(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

func ghCreatePR(ctx context.Context, dir, base, head, title, body string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "create", "--base", base, "--head", head, "--title", title, "--body-file", "-")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(body)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gh pr create: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	lines := strings.Fields(string(out))
	if len(lines) == 0 {
		return "", errors.New("gh pr create printed no URL")
	}
	return lines[len(lines)-1], nil
}
