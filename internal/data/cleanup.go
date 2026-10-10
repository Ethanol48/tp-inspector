package data

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CacheEntry describes what is cached for one assignment.
type CacheEntry struct {
	Slug       string
	DataBytes  int64    // API responses and traces
	Traces     int      // number of cached trace files
	Repos      []string // logins with a cloned repo
	ReposBytes int64
}

// ReposDir is where the clones of an assignment live.
func ReposDir(root, slug string) string { return filepath.Join(root, slug, "repos") }

// RepoDir is where one student's clone lives.
func RepoDir(root, slug, login string) string { return filepath.Join(ReposDir(root, slug), login) }

func dirSize(p string) int64 {
	var n int64
	_ = filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}

// ScanCache lists every assignment present in the cache root.
func ScanCache(root string) ([]CacheEntry, error) {
	dirs, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []CacheEntry
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		e := CacheEntry{Slug: d.Name()}
		items, _ := os.ReadDir(filepath.Join(root, d.Name()))
		for _, it := range items {
			p := filepath.Join(root, d.Name(), it.Name())
			if it.Name() == "repos" {
				repos, _ := os.ReadDir(p)
				for _, r := range repos {
					if r.IsDir() {
						e.Repos = append(e.Repos, r.Name())
					}
				}
				e.ReposBytes = dirSize(p)
				continue
			}
			if it.Name() == "traces" {
				traces, _ := os.ReadDir(p)
				e.Traces = len(traces)
			}
			e.DataBytes += dirSize(p)
		}
		sort.Strings(e.Repos)
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}

// removeInside deletes p after checking it is strictly inside root, so a bad
// slug or login can never escape the cache directory.
func removeInside(root, p string) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	absP, err := filepath.Abs(p)
	if err != nil {
		return err
	}
	if absP == absRoot || !strings.HasPrefix(absP, absRoot+string(filepath.Separator)) {
		return fmt.Errorf("refusing to delete %s: not inside %s", absP, absRoot)
	}
	return os.RemoveAll(absP)
}

// validName rejects slugs and logins that could point elsewhere.
func validName(s string) error {
	if s == "" || s == "." || s == ".." || strings.ContainsAny(s, `/\`) {
		return fmt.Errorf("invalid name %q", s)
	}
	return nil
}

// RemoveRepo deletes one student's clone.
func RemoveRepo(root, slug, login string) error {
	if err := validName(slug); err != nil {
		return err
	}
	if err := validName(login); err != nil {
		return err
	}
	return removeInside(ReposDir(root, slug), RepoDir(root, slug, login))
}

// RemoveRepos deletes every clone of an assignment.
func RemoveRepos(root, slug string) error {
	if err := validName(slug); err != nil {
		return err
	}
	return removeInside(filepath.Join(root, slug), ReposDir(root, slug))
}

// RemoveData deletes an assignment's cached API responses and traces, keeping
// its repos. The assignment directory goes away when nothing is left.
func RemoveData(root, slug string) error {
	if err := validName(slug); err != nil {
		return err
	}
	dir := filepath.Join(root, slug)
	items, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, it := range items {
		if it.Name() == "repos" {
			continue
		}
		if err := removeInside(root, filepath.Join(dir, it.Name())); err != nil {
			return err
		}
	}
	if rest, _ := os.ReadDir(dir); len(rest) == 0 {
		return removeInside(root, dir)
	}
	return nil
}

// RemoveAllRepos deletes the clones of every assignment.
func RemoveAllRepos(root string) error {
	entries, err := ScanCache(root)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if len(e.Repos) > 0 {
			if err := RemoveRepos(root, e.Slug); err != nil {
				return err
			}
		}
	}
	return nil
}

// RemoveEverything deletes the whole cache root.
func RemoveEverything(root string) error {
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil
	}
	// Remove the children so the check in removeInside applies to each.
	items, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, it := range items {
		if err := removeInside(root, filepath.Join(root, it.Name())); err != nil {
			return err
		}
	}
	return os.Remove(root)
}

// HumanBytes formats a size like "113M".
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%c", float64(n)/float64(div), "KMGT"[exp])
}
