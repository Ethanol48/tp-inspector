// Command tpi is a TUI to inspect EPITA assignments on Forge operator.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"

	"golang.org/x/term"

	"tp-inspector/internal/cri"
	"tp-inspector/internal/data"
	"tp-inspector/internal/forge"
	"tp-inspector/internal/tp"
	"tp-inspector/internal/ui"
)

func usage() {
	fmt.Fprint(os.Stderr, `tpi — statistics and plagiarism hints for an EPITA assignment

Usage:
  tpi [flags] <slug>          e.g. tpi prog-205-p-01-2030
  echo <slug> | tpi [flags]
  tpi --logins suspects.txt prog-205-p-01-2030
  tpi --select-group prog-206-p-01-2030   pick your classes first (F later)
  tpi login                   log in to operator and to the CRI API
                              (stores ~/.cri-tokens and ~/.cri-api-tokens)
  tpi clean                   choose cached TP data / cloned repos to delete
  tpi clean --repos [-y]      delete every cloned repo
  tpi clean --all [-y]        delete the whole cache (TP data and repos)
  tpi dump <path> [k=v...]    raw GET on the operator API (debugging)
  tpi get-group [<slug>]      pick CRI classes, print their members' logins
                              (experimental; <slug> keeps the classes that fit it)

Flags must come before the slug.

Flags:
`)
	flag.PrintDefaults()
}

func main() {
	flag.Usage = usage
	tpFlag := flag.String("tp", "", "assignment slug (alternative to the positional argument)")
	tenant := flag.String("tenant", "", "tenant slug (default: looked up in your activities)")
	refresh := flag.Bool("refresh", false, "ignore cached API responses")
	cacheDir := flag.String("cache", data.DefaultCacheRoot(), "cache directory")
	selectGroup := flag.Bool("select-group", false, "choose your group among the CRI classes that fit the activity when the TUI opens (F does it later)")
	loginsFile := flag.String("logins", "", "text file of logins (one per line, # comments): your group; Overview and Ranking start on them (toggle with f), the Group tab compares them with the promo, A in Repos clones them")
	flag.Parse()

	auth := forge.NewAuth()
	client := forge.NewClient(auth)
	args := flag.Args()

	if len(args) > 0 && args[0] == "clean" {
		exitOn(clean(*cacheDir, args[1:]))
		return
	}
	if len(args) > 0 && args[0] == "login" {
		// Two sessions: operator only accepts poulpy's client, which cannot
		// get the "api" scope of the CRI API (classes for F).
		exitOn(auth.Ensure())
		fmt.Fprintln(os.Stderr, "operator: valid token stored in", auth.File)
		criAuth, err := forge.NewCRIAuth()
		exitOn(err)
		exitOn(criAuth.Ensure())
		fmt.Fprintln(os.Stderr, "CRI API: valid token stored in", criAuth.File)
		return
	}
	criAuth, err := forge.NewCRIAuth()
	exitOn(err)
	criSrc := cri.NewSource(cri.NewClient(criAuth), *cacheDir, *refresh)

	if len(args) > 0 && args[0] == "get-group" {
		exitOn(getGroup(criAuth, criSrc, args[1:]))
		return
	}
	if len(args) > 1 && args[0] == "dump" {
		exitOn(auth.Ensure())
		dump(client, args[1], args[2:])
		return
	}

	raw := *tpFlag
	if raw == "" && len(args) > 0 {
		raw = args[0]
	}
	if raw == "" && !term.IsTerminal(int(os.Stdin.Fd())) {
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		raw = line
	}
	if strings.TrimSpace(raw) == "" {
		usage()
		os.Exit(2)
	}
	slug, err := tp.Parse(raw)
	exitOn(err)

	var list []string
	if *loginsFile != "" {
		list, err = readLogins(*loginsFile, slug.Raw)
		exitOn(err)
	}

	// Log in before the TUI takes over the terminal. The CRI API login is
	// only needed for F: later, it opens the browser without printing.
	exitOn(auth.Ensure())
	if *selectGroup {
		exitOn(criAuth.Ensure())
	}
	criAuth.Log = func(string) {}

	loader := &data.Loader{Client: client, Cache: &data.Cache{Root: *cacheDir, Refresh: *refresh}, Tenant: *tenant}
	m := ui.New(slug, loader, list)
	m.CRI, m.SelectGroup = criSrc, *selectGroup
	exitOn(ui.Run(m))
}

// readLogins reads one login per line; blank lines and # comments are
// ignored, and full group slugs or e-mail addresses are accepted too.
func readLogins(path, tpSlug string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		for _, l := range strings.FieldsFunc(line, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\r' }) {
			l = strings.ToLower(l)
			if i := strings.Index(l, "@"); i >= 0 {
				l = l[:i]
			}
			if i := strings.Index(l, tpSlug+"-"); i >= 0 {
				l = l[i+len(tpSlug)+1:]
			}
			if l != "" {
				out = append(out, l)
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s contains no login", path)
	}
	return out, nil
}

// clean implements `tpi clean [--all|--repos] [-y]`.
func clean(root string, args []string) error {
	fs := flag.NewFlagSet("clean", flag.ExitOnError)
	all := fs.Bool("all", false, "delete the whole cache: TP data and cloned repos")
	reposOnly := fs.Bool("repos", false, "delete every cloned repo, keep TP data")
	yes := fs.Bool("y", false, "do not ask for confirmation")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "Usage: tpi clean [--all | --repos] [-y]\n\nWithout flags, opens an interface to choose what to delete.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *all && *reposOnly {
		return fmt.Errorf("--all and --repos are exclusive")
	}
	if !*all && !*reposOnly {
		return ui.RunClean(root)
	}

	entries, err := data.ScanCache(root)
	if err != nil {
		return err
	}
	var bytes int64
	var nRepos int
	for _, e := range entries {
		bytes += e.ReposBytes
		nRepos += len(e.Repos)
		if *all {
			bytes += e.DataBytes
		}
	}

	what := fmt.Sprintf("%d cloned repos (%s)", nRepos, data.HumanBytes(bytes))
	if *all {
		what = fmt.Sprintf("the whole cache %s: %d assignments, %d repos (%s)", root, len(entries), nRepos, data.HumanBytes(bytes))
	}

	if len(entries) == 0 || (*reposOnly && nRepos == 0) {
		fmt.Fprintln(os.Stderr, "Nothing to delete.")
		return nil
	}

	if !*yes && !confirm("Delete "+what+"?") {
		fmt.Fprintln(os.Stderr, "Cancelled.")
		return nil
	}

	if *all {
		err = data.RemoveEverything(root)
	} else {
		err = data.RemoveAllRepos(root)
	}

	if err == nil {
		fmt.Fprintln(os.Stderr, "Deleted", what+".")
	}

	return err
}

// confirm asks a yes/no question on the terminal; the default is no.
func confirm(q string) bool {
	fmt.Fprint(os.Stderr, q+" [y/N] ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	a := strings.ToLower(strings.TrimSpace(line))
	return a == "y" || a == "yes"
}

// getGroup implements `tpi get-group [<slug>]`: the logins of the chosen
// classes go to stdout, one per line, so `tpi get-group > group.txt` feeds
// --logins. With a slug, only the classes that fit the activity are offered.
func getGroup(auth *forge.Auth, src *cri.Source, args []string) error {
	var filter cri.Filter
	if len(args) > 0 {
		slug, err := tp.Parse(args[0])
		if err != nil {
			return err
		}
		filter = cri.ActivityFilter(slug.Year, slug.Bimester)
	}
	if err := auth.Ensure(); err != nil {
		return err
	}
	logins, err := ui.RunGetGroup(src, filter)
	for _, l := range logins {
		fmt.Println(l)
	}
	return err
}

func exitOn(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "tpi:", err)
		os.Exit(1)
	}
}

func dump(c *forge.Client, path string, kv []string) {
	q := url.Values{}
	for _, s := range kv {
		k, v, _ := strings.Cut(s, "=")
		q.Set(k, v)
	}

	b, err := c.GetRaw(path, q)
	exitOn(err)

	var v any
	if json.Unmarshal(b, &v) == nil {
		b, _ = json.MarshalIndent(v, "", "  ")
	}

	os.Stdout.Write(append(b, '\n'))
}
