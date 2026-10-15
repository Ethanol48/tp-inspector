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
		return "", fmt.Errorf("git %s: %v: %s", args[0], err, gitMessage(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// gitMessage keeps the useful part of git's output: ssh banners such as
// "** WARNING: connection is not using a post-quantum key exchange" come
// before the actual error and hide it in a one-line status.
func gitMessage(out string) string {
	var keep []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "**") {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, " / ")
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
			return fmt.Errorf("tag %s not found in the repository", t.Ref)
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

// Commit is one commit of a checkout's history, oldest first in Log.
type Commit struct {
	Hash      string
	Time      time.Time
	Author    string // "name <email>"
	Committer string
	Subject   string
	Body      string // rest of the message, trailers included
	Added     int    // lines added to C / Python sources
	Deleted   int
}

// sourceExt are the files whose lines count in Commit.Added.
var sourceExt = map[string]bool{".c": true, ".h": true, ".py": true}

// Log returns the history leading to HEAD (the checked-out tag), oldest
// first, without merges.
func Log(dir string) ([]Commit, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Fields are separated by \x1f and the message ends with \x1e, since
	// bodies span lines; numstat lines follow.
	out, err := git(ctx, dir, "log", "--no-merges", "--reverse", "--numstat",
		"--format=%x00%H%x1f%ct%x1f%an <%ae>%x1f%cn <%ce>%x1f%s%x1f%b%x1e", "HEAD")
	if err != nil {
		return nil, err
	}
	var cs []Commit
	for _, block := range strings.Split(out, "\x00") {
		msg, stats, _ := strings.Cut(block, "\x1e")
		f := strings.SplitN(msg, "\x1f", 6)
		if len(f) < 6 {
			continue
		}
		var ts int64
		fmt.Sscan(f[1], &ts)
		c := Commit{Hash: f[0], Time: time.Unix(ts, 0), Author: f[2], Committer: f[3],
			Subject: f[4], Body: strings.TrimSpace(f[5])}
		for _, l := range strings.Split(strings.TrimSpace(stats), "\n") {
			n := strings.SplitN(l, "\t", 3)
			if len(n) < 3 || !sourceExt[strings.ToLower(filepath.Ext(n[2]))] {
				continue // binary ("-") or not source
			}
			var a, d int
			fmt.Sscan(n[0], &a)
			fmt.Sscan(n[1], &d)
			c.Added += a
			c.Deleted += d
		}
		cs = append(cs, c)
	}
	return cs, nil
}
