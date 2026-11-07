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
	"github.com/spf13/pflag"
	"golang.org/x/term"

	"tp-inspector/internal/config"
	"tp-inspector/internal/cri"
	"tp-inspector/internal/data"
	"tp-inspector/internal/forge"
	"tp-inspector/internal/tp"
	"tp-inspector/internal/ui"
)

// Global flags, shared by every command.
var (
	cacheDir   string
	refresh    bool
	configFile string
	cfg        config.Config // the config file, read before every command
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
	rootCmd.PersistentPreRunE = loadConfig // set here: loadConfig refers to rootCmd

	pf := rootCmd.PersistentFlags()
	cacheDir = data.DefaultCacheRoot()
	pf.Var(named(&cacheDir, "directory"), "cache", "cache directory")
	pf.BoolVar(&refresh, "refresh", false, "ignore cached API responses")
	pf.Var(named(&configFile, "file"), "config", "config file (default "+config.DefaultPath()+", or $"+config.EnvPath+")")
	_ = rootCmd.MarkPersistentFlagDirname("cache")
	_ = rootCmd.MarkPersistentFlagFilename("config", "toml")

	f := rootCmd.Flags()
	f.Var(named(&tpFlag, "slug"), "tp", "assignment slug (alternative to the argument)")
	f.Var(named(&tenant, "slug"), "tenant", "tenant slug (default: looked up in your activities)")
	f.Var(named(&loginsFile, "file"), "logins", "file of logins, one per line: your group")
	f.BoolVar(&selectGroup, "select-group", false, "pick your group among the CRI classes at start (F later)")
	registerCompletion(rootCmd, "tp", completeSlug)
	registerCompletion(rootCmd, "tenant", completeTenant)
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

	auth := newAuth()
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

// loadConfig reads the config file and applies its values to the flags
// that were not given on the command line.
func loadConfig(cmd *cobra.Command, args []string) error {
	if cmd == configInitCmd {
		return nil // must work even when the current file is broken
	}
	var err error
	if cfg, err = config.Load(config.Path(configFile)); err != nil {
		return err
	}
	set := func(p *string, flag *pflag.Flag, v string) {
		if v != "" && flag != nil && !flag.Changed {
			*p = v
		}
	}
	set(&cacheDir, rootCmd.PersistentFlags().Lookup("cache"), cfg.CacheDir)
	set(&tenant, rootCmd.Flags().Lookup("tenant"), cfg.Tenant)
	set(&loginsFile, rootCmd.Flags().Lookup("logins"), cfg.Logins)
	return ui.ApplyConfig(cfg)
}

// newAuth returns the operator session, with the token file of the config.
func newAuth() *forge.Auth {
	auth := forge.NewAuth()
	if cfg.Tokens.Operator != "" {
		auth.File = cfg.Tokens.Operator
	}
	return auth
}

// newCRIAuth returns the CRI API session, with the token file and client of
// the config (TPI_CRI_CLIENT_ID still takes precedence over the client).
func newCRIAuth() (*forge.Auth, error) {
	auth, err := forge.NewCRIAuth()
	if err != nil {
		return nil, err
	}
	if cfg.Tokens.CRIAPI != "" {
		auth.File = cfg.Tokens.CRIAPI
	}
	if cfg.CRIClient.ID != "" && os.Getenv("TPI_CRI_CLIENT_ID") == "" {
		auth.ClientID, auth.ClientSecret = cfg.CRIClient.ID, cfg.CRIClient.Secret
	}
	return auth, nil
}

// newCRI returns the CRI API session and the cached source of classes.
func newCRI() (*forge.Auth, *cri.Source, error) {
	auth, err := newCRIAuth()
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

// namedString is a string flag whose value is shown as name in the help
// ("--logins file"). The usual `name` in the usage text would also show its
// backticks in the descriptions of shell completion.
type namedString struct {
	p    *string
	name string
}

func named(p *string, name string) *namedString { return &namedString{p, name} }

func (n *namedString) String() string     { return *n.p }
func (n *namedString) Set(v string) error { *n.p = v; return nil }
func (n *namedString) Type() string       { return n.name }
