package ui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"tp-inspector/internal/data"
	"tp-inspector/internal/repos"
)

// cheatEnv optionally overrides the remote template learnt from the open
// assignment; placeholders: {login}, {slug} (activity), {promo} and {year}.
const cheatEnv = "TPI_REPO_URL"

// cheatDone is the step of the last progress message of a scan.
const cheatDone = "cheat-scan-done"

// cheatResult is the verdict on one TP: a commit with an AI co-author, or why
// the commits could not be read.
type cheatResult struct {
	cheat bool
	err   string
}

// coAuthorRe matches a "Co-authored-by: Name <email>" trailer.
var coAuthorRe = regexp.MustCompile(`(?im)^\s*co-authored-by:\s*(.+)$`)

// aiMarkers are lowercase fragments of the name or email of an AI co-author.
var aiMarkers = []string{
	"claude", "anthropic", "copilot", "chatgpt", "openai", "gpt", "codex",
	"gemini", "cursor", "aider", "devin", "codeium", "windsurf", "tabnine",
	"mistral", "deepseek", "llama", "perplexity", "grok",
}

// HasAICoAuthor reports whether a commit message has a Co-authored-by
// trailer naming an AI.
func HasAICoAuthor(message string) bool {
	for _, m := range coAuthorRe.FindAllStringSubmatch(message, -1) {
		who := strings.ToLower(m[1])
		for _, a := range aiMarkers {
			if strings.Contains(who, a) {
				return true
			}
		}
	}
	return false
}

// AnyAICoAuthor reports whether one of the commit messages has an AI
// co-author.
func AnyAICoAuthor(messages []string) bool {
	for _, m := range messages {
		if HasAICoAuthor(m) {
			return true
		}
	}
	return false
}

// deriveTemplate learns the remote pattern of the student's repositories from
// the one of the open assignment (the Group.GitRemoteURL the Repos tab clones):
// it contains the login and the slug of the assignment, replaced by
// placeholders so that the remote of any other TP of the history follows from
// its slug. TPI_REPO_URL, when set, wins. "" when nothing can be derived.
func deriveTemplate(remote, login, slug string, promo int) string {
	if v := strings.TrimSpace(os.Getenv(cheatEnv)); v != "" {
		return v
	}
	if remote == "" || !strings.Contains(remote, slug) {
		return ""
	}
	t := strings.ReplaceAll(remote, slug, "{slug}")
	if login != "" {
		t = strings.ReplaceAll(t, login, "{login}")
	}
	if promo > 0 { // a promo written apart from the slug
		t = strings.ReplaceAll(t, strconv.Itoa(promo), "{promo}")
	}
	return t
}

// repoURL builds the git remote of a student's activity from the template.
func repoURL(tmpl, login string, e data.HistoryEntry) string {
	sc, _ := sectionOf(e)
	return strings.NewReplacer(
		"{login}", login,
		"{slug}", e.Activity.Slug,
		"{promo}", fmt.Sprint(sc.promo),
		"{year}", fmt.Sprint(sc.year),
	).Replace(tmpl)
}

// checkTP downloads only the commit metadata of the TP of the student up to
// the tag they submitted (no trees, no blobs) and looks for an AI co-author in
// those commits. The bare repo goes to <root>/<slug>/meta/<login>.git: it is
// kept apart from <root>/<slug>/repos, which holds the full clones of the
// Repos tab.
func checkTP(ctx context.Context, tmpl, root, login string, e data.HistoryEntry) (cheatResult, error) {
	if tmpl == "" {
		return cheatResult{}, fmt.Errorf("no git remote known for this student in the open assignment")
	}
	if e.Last == nil || e.Last.Ref == "" {
		return cheatResult{}, fmt.Errorf("no submitted tag")
	}
	remote := repoURL(tmpl, login, e)
	dir := filepath.Join(root, e.Activity.Slug, "meta", login+".git")
	messages, err := fetchCommitMessages(ctx, dir, remote, e.Last.Ref)
	if err != nil {
		if repos.IsAuthError(err) {
			return cheatResult{}, fmt.Errorf("SSH refused your key: press K in Repos to unlock it (%s)", remote)
		}
		return cheatResult{}, fmt.Errorf("%s: %v", remote, err)
	}
	return cheatResult{cheat: AnyAICoAuthor(messages)}, nil
}

// fetchCommitMessages fetches the commits that lead to a tag with
// --filter=tree:0 (commits only) into a bare repo and returns their full
// messages. The repo is created on the first call and only updated after.
func fetchCommitMessages(ctx context.Context, dir, remote, ref string) ([]string, error) {
	if _, err := os.Stat(filepath.Join(dir, "HEAD")); err != nil {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		for _, args := range [][]string{
			{"init", "--bare", "--quiet"},
			{"remote", "add", "origin", remote},
			{"config", "remote.origin.promisor", "true"},
			{"config", "remote.origin.partialclonefilter", "tree:0"},
		} {
			if _, err := gitRun(ctx, dir, args...); err != nil {
				return nil, err
			}
		}
	} else if _, err := gitRun(ctx, dir, "remote", "set-url", "origin", remote); err != nil {
		return nil, err
	}

	tag := "refs/tags/" + ref
	// "+" so that a tag the student moved is updated.
	if _, err := gitRun(ctx, dir, "fetch", "--quiet", "--no-tags", "--filter=tree:0",
		"origin", "+"+tag+":"+tag); err != nil {
		return nil, err
	}
	out, err := gitRun(ctx, dir, "log", "--format=%B%x00", tag+"^{commit}")
	if err != nil {
		return nil, err
	}
	var msgs []string
	for _, m := range strings.Split(string(out), "\x00") {
		if m = strings.TrimSpace(m); m != "" {
			msgs = append(msgs, m)
		}
	}
	return msgs, nil
}

// gitRun runs git in dir without ever prompting (the TUI owns the terminal)
// and returns its stdout; the error carries stderr, which repos.IsAuthError
// reads.
func gitRun(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if os.Getenv("GIT_SSH_COMMAND") == "" {
		cmd.Env = append(cmd.Env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %v: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// scanCheats checks every TP with a submission and returns the verdict by
// activity slug.
func scanCheats(login, tmpl, root string, entries []data.HistoryEntry, progress func(done, total int)) map[string]cheatResult {
	var tps []data.HistoryEntry
	seen := map[string]bool{}
	for _, e := range entries {
		if kindOf(e) == "p" && e.Subs > 0 && !seen[e.Activity.Slug] {
			seen[e.Activity.Slug] = true
			tps = append(tps, e)
		}
	}
	res := make(map[string]cheatResult, len(tps))
	var mu sync.Mutex
	done := 0
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8) // metadata only: much lighter than a clone
	for _, e := range tps {
		wg.Add(1)
		sem <- struct{}{}
		go func(e data.HistoryEntry) {
			defer wg.Done()
			defer func() { <-sem }()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			r, err := checkTP(ctx, tmpl, root, login, e)
			if err != nil {
				r = cheatResult{err: err.Error()}
			}
			mu.Lock()
			res[e.Activity.Slug] = r
			done++
			d := done
			mu.Unlock()
			progress(d, len(tps))
		}(e)
	}
	wg.Wait()
	return res
}