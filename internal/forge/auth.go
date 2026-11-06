// Package forge talks to the Forge operator API.
package forge

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	authorizeURL = "https://cri.epita.fr/authorize"
	tokenURL     = "https://cri.epita.fr/token"
)

type tokens struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	ExpiresAt    int64  `json:"expires_at"` // added by tpi: the access token is opaque
}

// Auth provides valid tokens, refreshing or logging in when needed.
// Log messages go to Log (stderr by default), so Ensure must run before the TUI.
type Auth struct {
	File         string
	Log          func(string)
	ClientID     string
	ClientSecret string // empty for poulpy's public client
	Scope        string
	Port         int  // loopback port of the redirect URI, fixed by the client
	Access       bool // Token returns the access token instead of the ID token

	mu  sync.Mutex
	tok tokens
}

// NewAuth is the operator login: same OAuth2 PKCE flow as `poulpy auth`
// (poulpy/auth.py). The CRI only allows poulpy's client id to redirect to
// localhost:2097, and tokens are stored in ~/.cri-tokens so poulpy and tpi
// share them.
func NewAuth() *Auth {
	home, _ := os.UserHomeDir()
	return &Auth{
		File:     filepath.Join(home, ".cri-tokens"),
		Log:      func(s string) { fmt.Fprintln(os.Stderr, s) },
		ClientID: "825885",
		Scope:    "openid epita roles profile email",
		Port:     2097,
	}
}

// NewCRIAuth is the login for the CRI API (/api/v2), which needs the "api"
// scope. poulpy's client cannot request it, so it defaults to the Forge ID
// public test client (development only, id and secret published in
// https://docs.forge.epita.fr/services/forge-id/oidc#public-test-client).
// TPI_CRI_CLIENT_ID / TPI_CRI_CLIENT_SECRET override it; the client must
// allow the redirect URI http://localhost:8080/complete/epita/.
// Forge ID public test client, the default of the CRI API login.
const (
	TestClientID     = "125070"
	TestClientSecret = "f6ff8d394e6185d41834b19210979b897852680cf34700ae4ecb24ea"
)

func NewCRIAuth() (*Auth, error) {
	id, secret := os.Getenv("TPI_CRI_CLIENT_ID"), os.Getenv("TPI_CRI_CLIENT_SECRET")
	if id == "" {
		id, secret = TestClientID, TestClientSecret
	}
	home, _ := os.UserHomeDir()
	return &Auth{
		File:         filepath.Join(home, ".cri-api-tokens"),
		Log:          func(s string) { fmt.Fprintln(os.Stderr, s) },
		ClientID:     id,
		ClientSecret: secret,
		Scope:        "openid api",
		Port:         8080,
		Access:       true,
	}, nil
}

func (a *Auth) redirectURI() string {
	return fmt.Sprintf("http://localhost:%d/complete/epita/", a.Port)
}

func (a *Auth) current() string {
	if a.Access {
		return a.tok.AccessToken
	}
	return a.tok.IDToken
}

func (a *Auth) valid() bool {
	if a.Access {
		return a.tok.AccessToken != "" && time.Until(time.Unix(a.tok.ExpiresAt, 0)) > time.Minute
	}
	return a.tok.IDToken != "" && !expired(a.tok.IDToken)
}

// Ensure makes sure a non-expired token is available, interactively if needed.
func (a *Auth) Ensure() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.current() == "" {
		if b, err := os.ReadFile(a.File); err == nil {
			_ = json.Unmarshal(b, &a.tok)
		}
	}
	if a.valid() {
		return nil
	}
	if a.tok.RefreshToken != "" {
		if err := a.refresh(); err == nil {
			return nil
		}
		a.Log("Token refresh failed, you need to log in again.")
	}
	return a.login()
}

// Token returns the current bearer token.
func (a *Auth) Token() (string, error) {
	if err := a.Ensure(); err != nil {
		return "", err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.current(), nil
}

// Invalidate forces a refresh on the next Token call (after a 401).
func (a *Auth) Invalidate(old string) {
	a.mu.Lock()
	if a.current() == old {
		a.tok.IDToken, a.tok.AccessToken = "", ""
	}
	a.mu.Unlock()
}

// Username returns the CRI login of the session (preferred_username of the
// ID token), "" when unknown. It never triggers a login.
func (a *Auth) Username() string {
	a.mu.Lock()
	tok := a.tok.IDToken
	a.mu.Unlock()
	if tok == "" {
		var t tokens
		if b, err := os.ReadFile(a.File); err == nil && json.Unmarshal(b, &t) == nil {
			tok = t.IDToken
		}
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		PreferredUsername string `json:"preferred_username"`
	}
	_ = json.Unmarshal(payload, &claims)
	return claims.PreferredUsername
}

// expired reports whether the JWT expires within the next minute.
func expired(jwt string) bool {
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		return true
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return true
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Exp == 0 {
		return true
	}
	return time.Until(time.Unix(claims.Exp, 0)) < time.Minute
}

// store saves the token response as is (poulpy reads the same file), plus
// expires_at so the opaque access token can be checked without a request.
func (a *Auth) store(raw []byte) error {
	var t tokens
	var all map[string]any
	if err := json.Unmarshal(raw, &t); err != nil || t.IDToken == "" || t.RefreshToken == "" || json.Unmarshal(raw, &all) != nil {
		return fmt.Errorf("unexpected token response: %.200s", raw)
	}
	t.ExpiresAt = time.Now().Unix() + t.ExpiresIn
	all["expires_at"] = t.ExpiresAt
	b, err := json.Marshal(all)
	if err != nil {
		return err
	}
	a.tok = t
	return os.WriteFile(a.File, b, 0o600)
}

func (a *Auth) postToken(form url.Values) error {
	form.Set("client_id", a.ClientID)
	if a.ClientSecret != "" {
		form.Set("client_secret", a.ClientSecret)
	}
	resp, err := http.PostForm(tokenURL, form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return fmt.Errorf("token endpoint: %s", resp.Status)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token endpoint: %s: %.200s", resp.Status, raw)
	}
	return a.store(raw)
}

func (a *Auth) refresh() error {
	return a.postToken(url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {a.tok.RefreshToken},
	})
}

func (a *Auth) login() error {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return err
	}
	verifier := base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	q := url.Values{
		"response_type":         {"code"},
		"scope":                 {a.Scope},
		"client_id":             {a.ClientID},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"redirect_uri":          {a.redirectURI()},
	}
	authURL := authorizeURL + "?" + q.Encode()

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", a.Port))
	if err != nil {
		return fmt.Errorf("cannot listen on port %d: %w", a.Port, err)
	}
	codes := make(chan string, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/":
			http.Redirect(w, r, authURL, http.StatusFound)
		case strings.HasPrefix(r.URL.Path, "/complete/epita/") && r.URL.Query().Get("code") != "":
			fmt.Fprintln(w, "OK - you can close this tab now.")
			select {
			case codes <- r.URL.Query().Get("code"):
			default:
			}
		default:
			http.Error(w, "bad request", http.StatusBadRequest)
		}
	})}
	go srv.Serve(ln)
	defer srv.Close()

	local := fmt.Sprintf("http://localhost:%d", a.Port)
	a.Log(">>> Log in at: " + local)
	_ = exec.Command("xdg-open", local).Start()

	var code string
	select {
	case code = <-codes:
	case <-time.After(5 * time.Minute):
		return fmt.Errorf("login: timed out")
	}
	if err := a.postToken(url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {a.redirectURI()},
		"code_verifier": {verifier},
	}); err != nil {
		return err
	}
	a.Log(" OK: logged in.")
	return nil
}
