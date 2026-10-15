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

// Same OAuth2 PKCE flow as `poulpy auth` (poulpy/auth.py): the CRI only allows
// poulpy's client id to redirect to localhost:2097, and tokens are stored in
// ~/.cri-tokens so poulpy and tpi share them.
const (
	clientID     = "825885"
	callbackPort = 2097
	authorizeURL = "https://cri.epita.fr/authorize"
	tokenURL     = "https://cri.epita.fr/token"
)

var redirectURI = fmt.Sprintf("http://localhost:%d/complete/epita/", callbackPort)

type tokens struct {
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
}

// Auth provides valid JWTs, refreshing or logging in when needed.
// Log messages go to Log (stderr by default), so Ensure must run before the TUI.
type Auth struct {
	File string
	Log  func(string)

	mu  sync.Mutex
	tok tokens
}

func NewAuth() *Auth {
	home, _ := os.UserHomeDir()
	return &Auth{
		File: filepath.Join(home, ".cri-tokens"),
		Log:  func(s string) { fmt.Fprintln(os.Stderr, s) },
	}
}

// Ensure makes sure a non-expired JWT is available, interactively if needed.
func (a *Auth) Ensure() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tok.IDToken == "" {
		if b, err := os.ReadFile(a.File); err == nil {
			_ = json.Unmarshal(b, &a.tok)
		}
	}
	if a.tok.IDToken != "" && !expired(a.tok.IDToken) {
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

// Token returns the current JWT.
func (a *Auth) Token() (string, error) {
	if err := a.Ensure(); err != nil {
		return "", err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tok.IDToken, nil
}

// Invalidate forces a refresh on the next Token call (after a 401).
func (a *Auth) Invalidate(old string) {
	a.mu.Lock()
	if a.tok.IDToken == old {
		a.tok.IDToken = ""
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

func (a *Auth) store(raw []byte) error {
	var t tokens
	if err := json.Unmarshal(raw, &t); err != nil || t.IDToken == "" || t.RefreshToken == "" {
		return fmt.Errorf("unexpected token response: %.200s", raw)
	}
	a.tok = t
	return os.WriteFile(a.File, raw, 0o600)
}

func (a *Auth) postToken(form url.Values) error {
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
		"client_id":     {clientID},
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
		"scope":                 {"openid epita roles profile email"},
		"client_id":             {clientID},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"redirect_uri":          {redirectURI},
	}
	authURL := authorizeURL + "?" + q.Encode()

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", callbackPort))
	if err != nil {
		return fmt.Errorf("cannot listen on port %d: %w", callbackPort, err)
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

	local := fmt.Sprintf("http://localhost:%d", callbackPort)
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
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"code_verifier": {verifier},
	}); err != nil {
		return err
	}
	a.Log(" OK: logged in.")
	return nil
}
