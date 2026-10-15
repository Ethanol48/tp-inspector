package analysis

import (
	"fmt"
	"sort"
	"sync"

	"tp-inspector/internal/repos"
)

// CodeResult is the code analysis of every repo on disk.
type CodeResult struct {
	Repos  map[string]*Repo
	Pairs  []CodePair
	AI     map[string]AIScore
	Errors []string
}

// Analyze loads the repos (login -> checkout directory) and their history,
// then compares them. progress is called as repos are read.
func Analyze(dirs map[string]string, progress func(done, total int)) *CodeResult {
	res := &CodeResult{Repos: map[string]*Repo{}}
	history := map[string][]repos.Commit{}
	logins := make([]string, 0, len(dirs))
	for l := range dirs {
		logins = append(logins, l)
	}
	sort.Strings(logins)
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		done int
		sem  = make(chan struct{}, 8)
	)
	for _, l := range logins {
		wg.Add(1)
		sem <- struct{}{}
		go func(l string) {
			defer func() { <-sem; wg.Done() }()
			r, err := LoadRepo(l, dirs[l])
			h, herr := repos.Log(dirs[l])
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", l, err))
			} else {
				res.Repos[l] = r
			}
			if herr == nil {
				history[l] = h
			}
			done++
			if progress != nil {
				progress(done, len(logins))
			}
		}(l)
	}
	wg.Wait()
	rs := make([]*Repo, 0, len(res.Repos))
	for _, l := range logins {
		if r := res.Repos[l]; r != nil {
			rs = append(rs, r)
		}
	}
	res.Pairs = CompareRepos(rs)
	res.AI = AIScores(rs, history)
	return res
}

// Best returns, for each login, the best code pair score it is part of.
func (c *CodeResult) Best() map[string]float64 {
	out := map[string]float64{}
	if c == nil {
		return out
	}
	for _, p := range c.Pairs {
		for _, l := range []string{p.A, p.B} {
			out[l] = max(out[l], p.Score)
		}
	}
	return out
}
