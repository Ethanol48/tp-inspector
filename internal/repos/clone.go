// Package repos clones student repositories at the tag they submitted.
package repos

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Target is one repository to check out.
type Target struct {
	Login  string
	Remote string // git@... URL
	Ref    string // tag of the graded submission, e.g. "submit-1"
}

// Result is the outcome for one target.
type Result struct {
	Target
	Dir    string
	Commit string // "abc1234 2026-09-23 19:27 subject" of the checked-out commit
	Err    error
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "advice.detachedHead=false"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes -o ConnectTimeout=15",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// Sync makes dir a checkout of t.Ref, cloning or fetching only when needed.
func Sync(ctx context.Context, dir string, t Target) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	fresh := false
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		fresh = true
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return err
		}
		if _, err := git(ctx, filepath.Dir(dir), "clone", "-q", "--no-checkout", t.Remote, dir); err != nil {
			return err
		}
	}
	want := "refs/tags/" + t.Ref + "^{commit}"
	target, err := git(ctx, dir, "rev-parse", "-q", "--verify", want)
	if err != nil {
		if _, err := git(ctx, dir, "fetch", "-q", "--tags", "--force", "origin"); err != nil {
			return err
		}
		if target, err = git(ctx, dir, "rev-parse", "-q", "--verify", want); err != nil {
			return fmt.Errorf("tag %s no encontrado", t.Ref)
		}
	}
	// A --no-checkout clone has HEAD set but an empty work tree.
	if head, _ := git(ctx, dir, "rev-parse", "-q", "--verify", "HEAD"); head == target && !fresh {
		return nil
	}
	_, err = git(ctx, dir, "checkout", "-q", "-f", target)
	return err
}

// SyncAll checks out every target under root/<login> with limited parallelism.
func SyncAll(ctx context.Context, root string, targets []Target, workers int, progress func(done, total int)) []Result {
	results := make([]Result, len(targets))
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		done int
		sem  = make(chan struct{}, workers)
	)
	for i, t := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, t Target) {
			defer func() { <-sem; wg.Done() }()
			dir := filepath.Join(root, t.Login)
			var err error
			if ctx.Err() != nil {
				err = ctx.Err()
			} else {
				err = Sync(ctx, dir, t)
			}
			res := Result{Target: t, Dir: dir, Err: err}
			if err == nil {
				res.Commit, _ = git(ctx, dir, "log", "-1", "--format=%h %cd %s", "--date=format:%d/%m %H:%M")
			}
			results[i] = res
			mu.Lock()
			done++
			progress(done, len(targets))
			mu.Unlock()
		}(i, t)
	}
	wg.Wait()
	return results
}

// Inspect describes an existing clone: the tag at HEAD (if any) and the
// checked-out commit, formatted like Result.Commit.
func Inspect(dir string) (ref, commit string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if commit, err = git(ctx, dir, "log", "-1", "--format=%h %cd %s", "--date=format:%d/%m %H:%M"); err != nil {
		return "", "", err
	}
	ref, _ = git(ctx, dir, "describe", "--tags", "--exact-match", "HEAD")
	return ref, commit, nil
}
