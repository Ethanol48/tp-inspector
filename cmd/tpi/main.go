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
  tpi login                   log in to the CRI (stores ~/.cri-tokens)
  tpi clean                   choose cached TP data / cloned repos to delete
  tpi clean --repos [-y]      delete every cloned repo
  tpi clean --all [-y]        delete the whole cache (TP data and repos)
  tpi dump <path> [k=v...]    raw GET on the operator API (debugging)

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
	loginsFile := flag.String("logins", "", "text file of logins (one per line, # comments): the Ranking shows only them (toggle with f)")
	flag.Parse()

	auth := forge.NewAuth()
	client := forge.NewClient(auth)
	args := flag.Args()

	if len(args) > 0 && args[0] == "clean" {
		exitOn(clean(*cacheDir, args[1:]))
		return
	}
	if len(args) > 0 && args[0] == "login" {
		exitOn(auth.Ensure())
		fmt.Fprintln(os.Stderr, "valid token stored in", auth.File)
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

	// Log in before the TUI takes over the terminal.
	exitOn(auth.Ensure())

	loader := &data.Loader{Client: client, Cache: &data.Cache{Root: *cacheDir, Refresh: *refresh}, Tenant: *tenant}
	exitOn(ui.Run(ui.New(slug, loader, list)))
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
