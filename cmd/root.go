// Package cmd is the tpi command line, built with Cobra: the root command
// opens the TUI on an assignment, the subcommands handle login, cache
// cleaning and debugging. `tpi completion <shell>` prints the completion script.
package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"tp-inspector/internal/cri"
	"tp-inspector/internal/data"
	"tp-inspector/internal/forge"
	"tp-inspector/internal/tp"
	"tp-inspector/internal/ui"
)

// Global flags, shared by every command.
var (
	cacheDir string
	refresh  bool
)

// Flags of the root command (the TUI).
var (
	tpFlag      string
	tenant      string
	loginsFile  string
	selectGroup bool
)

var rootCmd = &cobra.Command{
	Use:   "tpi [flags] <slug>",
	Short: "Statistics and plagiarism hints for an EPITA assignment",
	Long: `tpi — statistics and plagiarism hints for an EPITA assignment.

Opens a TUI on an assignment of Forge operator: overview, ranking, traces,
history, cloned repos and suspicion hints. The slug can also come from stdin.

The group (--logins, --select-group or F in the TUI) is what Overview and
Ranking start on (toggle with f), what the Group tab compares with the promo
and what A clones in Repos.`,
	Example: `  # open the TUI on an assignment
  tpi prog-205-p-01-2030
  echo prog-205-p-01-2030 | tpi

  # focus on your group: a file of logins, or CRI classes picked at start
  tpi --logins my-group.txt prog-205-p-01-2030
  tpi --select-group prog-206-p-01-2030`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completeSlug,
	SilenceUsage:      true,
	SilenceErrors:     true,
	RunE:              runTUI,
}

// Execute runs the command line; errors are printed as "tpi: <error>".
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "tpi:", err)
		os.Exit(1)
	}
}

func init() {
	pf := rootCmd.PersistentFlags()
	pf.StringVar(&cacheDir, "cache", data.DefaultCacheRoot(), "cache `directory`")
	pf.BoolVar(&refresh, "refresh", false, "ignore cached API responses")
	_ = rootCmd.MarkPersistentFlagDirname("cache")

	f := rootCmd.Flags()
	f.StringVar(&tpFlag, "tp", "", "assignment `slug` (alternative to the argument)")
	f.StringVar(&tenant, "tenant", "", "tenant `slug` (default: looked up in your activities)")
	f.StringVar(&loginsFile, "logins", "", "`file` of logins, one per line: your group")
	f.BoolVar(&selectGroup, "select-group", false, "pick your group among the CRI classes at start (F later)")
	_ = rootCmd.RegisterFlagCompletionFunc("tp", completeSlug)
	_ = rootCmd.RegisterFlagCompletionFunc("tenant", completeTenant)
	_ = rootCmd.MarkFlagFilename("logins")
}

// runTUI opens the TUI on the slug given as argument, with --tp or on stdin.
func runTUI(cmd *cobra.Command, args []string) error {
	raw := tpFlag
	if raw == "" && len(args) > 0 {
		raw = args[0]
	}
	if raw == "" && !term.IsTerminal(int(os.Stdin.Fd())) {
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		raw = line
	}
	if strings.TrimSpace(raw) == "" {
		return cmd.Help()
	}
	slug, err := tp.Parse(raw)
	if err != nil {
		return err
	}

	var list []string
	if loginsFile != "" {
		if list, err = readLogins(loginsFile, slug.Raw); err != nil {
			return err
		}
	}

	auth := forge.NewAuth()
	criAuth, criSrc, err := newCRI()
	if err != nil {
		return err
	}

	// Log in before the TUI takes over the terminal. The CRI API login is
	// only needed for F: later, it opens the browser without printing.
	if err := auth.Ensure(); err != nil {
		return err
	}
	if selectGroup {
		if err := criAuth.Ensure(); err != nil {
			return err
		}
	}
	criAuth.Log = func(string) {}

	loader := &data.Loader{Client: forge.NewClient(auth), Cache: &data.Cache{Root: cacheDir, Refresh: refresh}, Tenant: tenant}
	m := ui.New(slug, loader, list)
	m.CRI, m.SelectGroup = criSrc, selectGroup
	return ui.Run(m)
}

// newCRI returns the CRI API session and the cached source of classes.
func newCRI() (*forge.Auth, *cri.Source, error) {
	auth, err := forge.NewCRIAuth()
	if err != nil {
		return nil, nil, err
	}
	return auth, cri.NewSource(cri.NewClient(auth), cacheDir, refresh), nil
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
