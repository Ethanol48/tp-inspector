// Package web serves a local, read-only web view of the cloned student repos.
package web

import (
	"archive/tar"
	"compress/gzip"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode/utf8"

	"tp-inspector/internal/cri"
)

//go:embed index.html
var static embed.FS

const (
	maxFileSize = 1 << 20 // larger files are not shown
	maxFiles    = 5000
)

var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "target": true,
	"build": true, "__pycache__": true, ".venv": true, "dist": true,
}

type server struct {
	root, tracesURL string
	classes         *cri.Source // nil: no class filter
	filter          cri.Filter
}

// Start serves the repos found in root (one directory per login) on addr,
// e.g. "127.0.0.1:0" for a free port. tracesURL is the Forge traces page of the
// assignment; the page appends ?q=<login>. classes and filter feed the class
// filter of the page. It returns the URL and a stop function.
func Start(root, addr, tracesURL string, classes *cri.Source, filter cri.Filter) (string, func(), error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", nil, err
	}
	s := &server{root: root, tracesURL: tracesURL, classes: classes, filter: filter}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/info", s.info)
	mux.HandleFunc("/api/repos", s.repos)
	mux.HandleFunc("/api/tree", s.tree)
	mux.HandleFunc("/api/file", s.file)
	mux.HandleFunc("/api/archive", s.archive)
	mux.HandleFunc("/api/classes", s.classList)
	mux.HandleFunc("/api/members", s.members)
	mux.Handle("/", http.FileServerFS(static))
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	return "http://" + ln.Addr().String(), func() { srv.Close() }, nil
}

func Open(url string) error {
	var candidates [][]string
	if b := os.Getenv("BROWSER"); b != "" {
		candidates = append(candidates, []string{b})
	}
	if runtime.GOOS == "darwin" {
		// "open" only on macOS: on some Linux distros it is an alias of openvt.
		candidates = append(candidates,
			[]string{"open"},
			[]string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"},
			[]string{"/Applications/Firefox.app/Contents/MacOS/firefox"},
			[]string{"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser"},
			[]string{"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"},
			[]string{"/Applications/Chromium.app/Contents/MacOS/Chromium"},
			[]string{"/Applications/Safari.app/Contents/MacOS/Safari"},
		)
	}
	candidates = append(candidates,
		// WSL / Windows
		[]string{"wslview"},
		[]string{"explorer.exe"},
		[]string{"/mnt/c/Windows/explorer.exe"},
		[]string{"/mnt/c/Program Files/Google/Chrome/Application/chrome.exe"},
		[]string{"/mnt/c/Program Files (x86)/Google/Chrome/Application/chrome.exe"},
		[]string{"/mnt/c/Program Files/Mozilla Firefox/firefox.exe"},
		[]string{"cmd.exe", "/c", "start", ""},
		// Linux
		[]string{"xdg-open"},
		[]string{"gio", "open"},
		[]string{"gnome-open"},
		[]string{"kde-open"},
		[]string{"sensible-browser"},
		[]string{"x-www-browser"},
		[]string{"google-chrome"}, []string{"google-chrome-stable"},
		[]string{"chromium"}, []string{"chromium-browser"},
		[]string{"firefox"}, []string{"brave-browser"}, []string{"microsoft-edge"},
		[]string{"vivaldi"}, []string{"opera"}, []string{"epiphany"}, []string{"falkon"},
	)
	for _, c := range candidates {
		path, err := exec.LookPath(c[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(path, append(c[1:], url)...)
		if err := cmd.Start(); err != nil {
			continue
		}
		_ = cmd.Process.Release()
		return nil
	}
	return errors.New("no browser found")
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// repoDir resolves a login to its directory, refusing anything but a plain name.
func (s *server) repoDir(login string) (string, bool) {
	if login == "" || login == "." || login == ".." || filepath.Base(login) != login {
		return "", false
	}
	dir := filepath.Join(s.root, login)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return "", false
	}
	return dir, true
}

// info exposes the traces URL and the repos root.
func (s *server) info(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"traces": s.tracesURL, "root": s.root})
}

type classReply struct {
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	Campus   string `json:"campus"`
	Promo    string `json:"promo"`
	Rank     int    `json:"rank"` // program order: prepa, ING, apprentices, cyber
	Semester string `json:"semester"`
	Code     string `json:"code"`
	Fits     bool   `json:"fits"` // can take this activity
}

// classList returns the CRI classes (cached by cri.Source) and which ones fit
// the activity.
func (s *server) classList(w http.ResponseWriter, r *http.Request) {
	if s.classes == nil {
		http.Error(w, "the CRI API is not available", http.StatusServiceUnavailable)
		return
	}
	cat, err := s.classes.Catalog()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	campuses := map[string]string{"": "Other"}
	for _, c := range cat.Campuses {
		campuses[c.Slug] = c.Name
	}
	out := make([]classReply, len(cat.Classes))
	for i, c := range cat.Classes {
		out[i] = classReply{Slug: c.Slug, Name: c.Name, Campus: c.Campus, Promo: c.Promo, Rank: cri.ProgramRank(c.Program),
			Semester: c.SemesterLabel(), Code: c.Code, Fits: s.filter.Match(c)}
	}
	writeJSON(w, map[string]any{"filter": s.filter.Label(), "campuses": campuses, "classes": out})
}

// members returns the logins of the classes given as ?slug= (repeatable).
func (s *server) members(w http.ResponseWriter, r *http.Request) {
	if s.classes == nil {
		http.Error(w, "the CRI API is not available", http.StatusServiceUnavailable)
		return
	}
	logins, err := s.classes.MembersOf(r.URL.Query()["slug"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if logins == nil {
		logins = []string{}
	}
	writeJSON(w, logins)
}

func (s *server) repos(w http.ResponseWriter, r *http.Request) {
	out := []string{}
	if entries, err := os.ReadDir(s.root); err == nil {
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				out = append(out, e.Name())
			}
		}
	}
	sort.Strings(out)
	writeJSON(w, out)
}

func (s *server) tree(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.repoDir(r.URL.Query().Get("login"))
	if !ok {
		http.Error(w, "unknown repo", http.StatusNotFound)
		return
	}
	files := []string{}
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != dir && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if len(files) >= maxFiles {
			return filepath.SkipAll
		}
		if rel, err := filepath.Rel(dir, p); err == nil {
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(files)
	writeJSON(w, files)
}

type fileReply struct {
	Path   string `json:"path"`
	Text   string `json:"text"`
	Binary bool   `json:"binary,omitempty"`
	Big    bool   `json:"big,omitempty"`
}

func (s *server) file(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.repoDir(r.URL.Query().Get("login"))
	if !ok {
		http.Error(w, "unknown repo", http.StatusNotFound)
		return
	}
	rel := filepath.Clean("/" + r.URL.Query().Get("path"))[1:]
	if rel == "" || rel == ".git" || strings.HasPrefix(rel, ".git/") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	full, err := filepath.EvalSymlinks(filepath.Join(dir, rel))
	base, _ := filepath.EvalSymlinks(dir)
	if err != nil || !strings.HasPrefix(full, base+string(filepath.Separator)) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	fi, err := os.Stat(full)
	if err != nil || fi.IsDir() {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	reply := fileReply{Path: filepath.ToSlash(rel)}
	if fi.Size() > maxFileSize {
		reply.Big = true
		writeJSON(w, reply)
		return
	}
	b, err := os.ReadFile(full)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	head := b[:min(len(b), 8192)]
	if strings.IndexByte(string(head), 0) >= 0 || !utf8.Valid(b) {
		reply.Binary = true
	} else {
		reply.Text = string(b)
	}
	writeJSON(w, reply)
}

// archive streams the repo of ?login= as a .tar.gz (the .git directory is
// included, heavy build directories are not). Symlinks are stored, never followed.
func (s *server) archive(w http.ResponseWriter, r *http.Request) {
	login := r.URL.Query().Get("login")
	dir, ok := s.repoDir(login)
	if !ok {
		http.Error(w, "unknown repo", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+login+`.tar.gz"`)
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	defer func() { tw.Close(); gz.Close() }()

	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil || rel == "." {
			return nil
		}
		if d.IsDir() && d.Name() != ".git" && skipDirs[d.Name()] {
			return filepath.SkipDir
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		mode := info.Mode()
		if !mode.IsRegular() && !mode.IsDir() && mode&os.ModeSymlink == 0 {
			return nil // sockets, devices...
		}
		link := ""
		if mode&os.ModeSymlink != 0 {
			if link, err = os.Readlink(p); err != nil {
				return nil
			}
		}
		h, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return nil
		}
		h.Name = login + "/" + filepath.ToSlash(rel)
		if mode.IsDir() {
			h.Name += "/"
		}
		if err := tw.WriteHeader(h); err != nil {
			return err // client disconnected
		}
		if mode.IsRegular() {
			f, err := os.Open(p)
			if err != nil {
				return nil
			}
			defer f.Close()
			if _, err := io.Copy(tw, f); err != nil {
				return err
			}
		}
		return nil
	})
}