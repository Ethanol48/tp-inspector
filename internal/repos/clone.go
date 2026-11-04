// Package repos clones student repositories at the tag they submitted.
//
// Debug / tuning without recompiling (environment variables):
//
//	REPOS_DEBUG=1        print the duration of every git command on stderr
//	REPOS_WORKERS=N      override the number of parallel workers
//	REPOS_FILTER=spec    override the partial-clone filter ("none" disables it)
//	REPOS_DEPTH=N        shallow fetch of N commits (0 = full history). Log()
//	                     then only sees those commits: see Deepen.
//	REPOS_SSH_EXTRA=...  extra ssh options, e.g. "-o AddressFamily=inet -i ~/.ssh/key"
package repos

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
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

// Options tunes SyncAllWith. Zero values fall back to DefaultOptions.
type Options struct {
	// Workers is the number of repositories processed in parallel. Each
	// worker owns one persistent SSH connection reused for all its repos.
	Workers int
	// Filter is a git partial-clone filter ("" disables it). It skips
	// blobs bigger than the limit at fetch time; the ones needed by the
	// checked-out tag are still downloaded at checkout.
	Filter string
	// Depth > 0 fetches only that many commits for a fresh clone. This is
	// the fastest mode but Log() will return a truncated history; call
	// Deepen on the repos whose full history you need.
	Depth int
	// Retries is the number of extra attempts after a transient failure.
	// A missing tag is never retried.
	Retries int
	// Timeout bounds the first attempt for one repository. Attempt n gets
	// n*Timeout, so a stuck connection is cut early and retried instead of
	// making the whole batch wait for the slowest repository.
	Timeout time.Duration
	// RampUp staggers the start of the workers (worker i starts after
	// i*RampUp), so that hundreds of SSH handshakes don't hit the server at
	// the same instant (sshd's MaxStartups drops them). Negative disables.
	RampUp time.Duration
	// SSHExtra is appended to the ssh command line.
	SSHExtra string
}

// DefaultOptions is tuned for a ~1 Gbps connection to GitHub/GitLab.
func DefaultOptions() Options {
	return Options{
		Workers: 64,
		Filter:  "blob:limit=2m",
		Retries: 2,
		Timeout: 60 * time.Second,
		RampUp:  20 * time.Millisecond,
	}
}

func (o Options) withDefaults() Options {
	d := DefaultOptions()
	if o.Workers <= 0 {
		o.Workers = d.Workers
	}
	if o.Retries < 0 {
		o.Retries = 0
	}
	if o.Timeout <= 0 {
		o.Timeout = d.Timeout
	}
	if o.RampUp == 0 {
		o.RampUp = d.RampUp
	} else if o.RampUp < 0 {
		o.RampUp = 0
	}
	return o
}

// withEnv applies the REPOS_* overrides, so that a value hard-coded by the
// caller (e.g. workers=4) can be changed for an experiment.
func (o Options) withEnv() Options {
	if v, err := strconv.Atoi(os.Getenv("REPOS_WORKERS")); err == nil && v > 0 {
		o.Workers = v
	}
	if v, err := strconv.Atoi(os.Getenv("REPOS_DEPTH")); err == nil && v >= 0 {
		o.Depth = v
	}
	if v := os.Getenv("REPOS_FILTER"); v != "" {
		if v == "none" {
			v = ""
		}
		o.Filter = v
	}
	if v := os.Getenv("REPOS_SSH_EXTRA"); v != "" {
		o.SSHExtra = v
	}
	return o
}

var debug = os.Getenv("REPOS_DEBUG") != ""

// baseSSH is the ssh command used when no multiplexing is set up.
const baseSSH = "ssh -o BatchMode=yes -o ConnectTimeout=15" +
	" -o Compression=no -o IPQoS=throughput" + // git packs are already compressed
	" -o GSSAPIAuthentication=no" + // avoids multi-second Kerberos lookups
	" -o ServerAliveInterval=15 -o ServerAliveCountMax=3"

// sshCommand returns the GIT_SSH_COMMAND of one worker. With a control
// directory, the worker keeps a single multiplexed connection alive across
// its successive repositories (one handshake instead of one per repo).
func sshCommand(cmDir string, slot int, extra string) string {
	s := baseSSH
	if cmDir != "" && runtime.GOOS != "windows" {
		s += fmt.Sprintf(" -o ControlMaster=auto -o ControlPersist=30s -o ControlPath=%s",
			filepath.Join(cmDir, fmt.Sprintf("%d-%%C", slot)))
	}
	if extra != "" {
		s += " " + extra
	}
	return s
}

// gitSSH runs git in dir using the given GIT_SSH_COMMAND ("" = default).
func gitSSH(ctx context.Context, dir, ssh string, args ...string) (string, error) {
	if ssh == "" {
		ssh = baseSSH
	}
	pre := []string{
		"-c", "advice.detachedHead=false",
		"-c", "gc.auto=0", // no background gc while cloning
		"-c", "maintenance.auto=false",
		"-c", "core.fsmonitor=false",
		"-c", "core.fsync=none", // scratch checkouts: skip fsync
		// Keep the received pack as is instead of exploding it into
		// thousands of loose files: far fewer file creations (big win on
		// Windows/macOS and with antivirus scanning).
		"-c", "fetch.unpackLimit=1",
		"-c", "transfer.unpackLimit=1",
		// Dozens of git processes run at once: don't let each index-pack
		// spawn one thread per core.
		"-c", "index-pack.threads=2",
	}
	cmd := exec.CommandContext(ctx, "git", append(pre, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND="+ssh,
	)
	start := time.Now()
	out, err := cmd.CombinedOutput()
	if debug {
		status := "ok"
		if err != nil {
			status = "ERR"
		}
		fmt.Fprintf(os.Stderr, "%7.2fs %-3s git %s  [%s]\n",
			time.Since(start).Seconds(), status, strings.Join(args, " "), filepath.Base(dir))
	}
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", args[0], err, gitMessage(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// git runs a local git command (no remote access).
func git(ctx context.Context, dir string, args ...string) (string, error) {
	return gitSSH(ctx, dir, "", args...)
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

type tagNotFoundError struct{ ref string }

func (e *tagNotFoundError) Error() string {
	return fmt.Sprintf("tag %s not found in the repository", e.ref)
}

// Sync makes dir a checkout of t.Ref. Only the history reachable from the
// tag is downloaded (no other branches, no other tags), in a single fetch.
// ssh is the GIT_SSH_COMMAND to use ("" for the default).
func Sync(ctx context.Context, dir string, t Target, opt Options, ssh string) error {
	opt = opt.withDefaults()
	ctx, cancel := context.WithTimeout(ctx, opt.Timeout)
	defer cancel()

	fresh := false
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		fresh = true
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		initRepo := func() error {
			if _, err := git(ctx, dir, "init", "-q"); err != nil {
				return err
			}
			_, err := git(ctx, dir, "remote", "add", "origin", t.Remote)
			return err
		}
		if err := initRepo(); err != nil {
			os.RemoveAll(dir) // don't leave a half-initialised repo behind
			return err
		}
	}

	want := "refs/tags/" + t.Ref + "^{commit}"
	var (
		target string
		err    error
	)
	if !fresh { // a fresh repo has nothing to look up
		target, err = git(ctx, dir, "rev-parse", "-q", "--verify", want)
	}
	if fresh || err != nil {
		args := []string{"fetch", "-q", "--no-tags", "--force"}
		if opt.Filter != "" {
			args = append(args, "--filter="+opt.Filter)
		}
		if fresh && opt.Depth > 0 {
			args = append(args, "--depth="+strconv.Itoa(opt.Depth))
		}
		args = append(args, "origin", "+refs/tags/"+t.Ref+":refs/tags/"+t.Ref)
		if _, err := gitSSH(ctx, dir, ssh, args...); err != nil {
			if strings.Contains(err.Error(), "couldn't find remote ref") {
				return &tagNotFoundError{t.Ref}
			}
			return err
		}
		if target, err = git(ctx, dir, "rev-parse", "-q", "--verify", want); err != nil {
			return &tagNotFoundError{t.Ref}
		}
	}
	// A fresh repo has an unborn HEAD, so only look at HEAD for an existing
	// clone, to skip a checkout that is already done.
	if !fresh {
		if head, _ := git(ctx, dir, "rev-parse", "-q", "--verify", "HEAD"); head == target {
			return nil
		}
	}
	// Missing blobs (partial clone) are fetched here, in one batch.
	_, err = gitSSH(ctx, dir, ssh, "checkout", "-q", "-f", target)
	return err
}

// Deepen turns a shallow clone (made with Options.Depth > 0) into one with
// the full history of the tag, so that Log() is complete. It does nothing
// on a repository that is already complete.
func Deepen(ctx context.Context, dir, ref, ssh string) error {
	if s, err := git(ctx, dir, "rev-parse", "--is-shallow-repository"); err != nil || s != "true" {
		return err
	}
	_, err := gitSSH(ctx, dir, ssh, "fetch", "-q", "--no-tags", "--unshallow", "origin",
		"+refs/tags/"+ref+":refs/tags/"+ref)
	return err
}

// syncRetry calls Sync, retrying transient failures with a short backoff and
// a longer timeout at each attempt.
func syncRetry(ctx context.Context, dir string, t Target, opt Options, ssh string) error {
	var err error
	for attempt := 0; attempt <= opt.Retries; attempt++ {
		if attempt > 0 {
			if debug {
				fmt.Fprintf(os.Stderr, "retry %d for %s: %v\n", attempt, t.Login, err)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}
		o := opt
		o.Timeout = opt.Timeout * time.Duration(attempt+1)
		err = Sync(ctx, dir, t, o, ssh)
		var nf *tagNotFoundError
		if err == nil || errors.As(err, &nf) || ctx.Err() != nil {
			return err
		}
	}
	return err
}

// SyncAll checks out every target under root/<login> with DefaultOptions
// and the given parallelism (<= 0 means the default; REPOS_WORKERS wins).
func SyncAll(ctx context.Context, root string, targets []Target, workers int, progress func(done, total int)) []Result {
	opt := DefaultOptions()
	if workers > 0 {
		opt.Workers = workers
	}
	return SyncAllWith(ctx, root, targets, opt, progress)
}

// SyncAllWith is SyncAll with explicit options.
func SyncAllWith(ctx context.Context, root string, targets []Target, opt Options, progress func(done, total int)) []Result {
	opt = opt.withDefaults().withEnv()
	results := make([]Result, len(targets))
	if len(targets) == 0 {
		return results
	}
	workers := opt.Workers
	if workers > len(targets) {
		workers = len(targets)
	}
	if debug {
		fmt.Fprintf(os.Stderr, "repos: %d targets, %d workers, filter=%q, depth=%d\n",
			len(targets), workers, opt.Filter, opt.Depth)
		defer func(start time.Time) {
			fmt.Fprintf(os.Stderr, "repos: total %.1fs\n", time.Since(start).Seconds())
		}(time.Now())
	}

	// Short directory for the SSH control sockets (unix socket paths are
	// limited to ~104 chars, and macOS' default temp dir is already long).
	cmDir := ""
	if runtime.GOOS != "windows" {
		base := "/tmp"
		if st, err := os.Stat(base); err != nil || !st.IsDir() {
			base = os.TempDir()
		}
		if d, err := os.MkdirTemp(base, "cm"); err == nil {
			cmDir = d
			defer os.RemoveAll(d)
		}
	}

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		done int
		jobs = make(chan int)
	)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			// Staggered start: avoid a burst of simultaneous SSH handshakes.
			if d := time.Duration(slot) * opt.RampUp; d > 0 {
				select {
				case <-ctx.Done():
				case <-time.After(d):
				}
			}
			ssh := sshCommand(cmDir, slot, opt.SSHExtra)
			for i := range jobs {
				t := targets[i]
				dir := filepath.Join(root, t.Login)
				var err error
				if ctx.Err() != nil {
					err = ctx.Err()
				} else {
					err = syncRetry(ctx, dir, t, opt, ssh)
				}
				res := Result{Target: t, Dir: dir, Err: err}
				if err == nil {
					res.Commit, _ = git(ctx, dir, "log", "-1", "--format=%h %cd %s", "--date=format:%d/%m %H:%M")
				}
				results[i] = res
				if progress != nil {
					mu.Lock()
					done++
					progress(done, len(targets))
					mu.Unlock()
				}
			}
		}(w)
	}
	for i := range targets {
		jobs <- i
	}
	close(jobs)
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