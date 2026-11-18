package ui

import (
	"context"
	"fmt"
	"os"
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

// checkTP clones the TP of the student at the tag they submitted, with the
// repos package, and looks for an AI co-author in the commits that lead to
// that tag. The clone goes to <root>/<slug>/repos/<login>, like the Repos tab.
func checkTP(ctx context.Context, tmpl, root, login string, e data.HistoryEntry) (cheatResult, error) {
	if tmpl == "" {
		return cheatResult{}, fmt.Errorf("no git remote known for this student in the open assignment")
	}
	if e.Last == nil || e.Last.Ref == "" {
		return cheatResult{}, fmt.Errorf("no submitted tag")
	}
	t := repos.Target{Login: login, Remote: repoURL(tmpl, login, e), Ref: e.Last.Ref}
	opt := repos.DefaultOptions()
	opt.Workers = 1
	res := repos.SyncAllWith(ctx, filepath.Join(root, e.Activity.Slug, "repos"), []repos.Target{t}, opt, nil)
	if err := res[0].Err; err != nil {
		if repos.IsAuthError(err) {
			return cheatResult{}, fmt.Errorf("SSH refused your key: press K in Repos to unlock it (%s)", t.Remote)
		}
		return cheatResult{}, fmt.Errorf("%s: %v", t.Remote, err)
	}
	// REPOS_DEPTH may have made a shallow clone: the whole history is needed.
	if err := repos.Deepen(ctx, res[0].Dir, t.Ref, ""); err != nil {
		return cheatResult{}, err
	}
	commits, err := repos.Log(res[0].Dir)
	if err != nil {
		return cheatResult{}, err
	}
	for _, c := range commits {
		if HasAICoAuthor(c.Subject + "\n\n" + c.Body) {
			return cheatResult{cheat: true}, nil
		}
	}
	return cheatResult{}, nil
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
	sem := make(chan struct{}, 4)
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