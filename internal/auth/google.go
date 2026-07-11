// Package auth implements the Google OAuth2 PKCE browser login flow for habctl.
// The user needs a Google Cloud OAuth2 "Desktop app" client (one-time setup).
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"golang.org/x/oauth2"
	googleoauth "golang.org/x/oauth2/google"
)

const geminiScope = "https://www.googleapis.com/auth/generative-language"

func geminiConfig(clientID, clientSecret string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scopes:       []string{geminiScope},
		Endpoint:     googleoauth.Endpoint,
	}
}

// BrowserLogin starts a local HTTP server, opens the browser to Google's OAuth
// consent page, waits for the callback, and returns the refresh token.
// The refresh token is long-lived and should be saved to config.
func BrowserLogin(clientID, clientSecret string) (refreshToken string, err error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("lokalen Port nicht belegen: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	redirectURL := fmt.Sprintf("http://localhost:%d/callback", port)

	conf := geminiConfig(clientID, clientSecret)
	conf.RedirectURL = redirectURL

	verifier := pkceVerifier()
	challenge := pkceChallenge(verifier)
	state := randB64(16)

	authURL := conf.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("prompt", "consent"),
	)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	srv := &http.Server{Handler: mux}

	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			errCh <- fmt.Errorf("OAuth state mismatch (CSRF?)")
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "no code", http.StatusBadRequest)
			errCh <- fmt.Errorf("kein Auth-Code empfangen")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body style="font-family:sans-serif;padding:2rem;max-width:400px">
<h2 style="color:#16a34a">✓ habctl: Login erfolgreich</h2>
<p>Du kannst diesen Tab schließen und zu habctl zurückkehren.</p>
</body></html>`)
		codeCh <- code
	})

	go srv.Serve(ln)
	defer srv.Shutdown(context.Background())

	OpenBrowser(authURL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var code string
	select {
	case code = <-codeCh:
	case e := <-errCh:
		return "", e
	case <-ctx.Done():
		return "", fmt.Errorf("Login-Timeout (5 Minuten abgelaufen)")
	}

	tok, err := conf.Exchange(ctx, code,
		oauth2.SetAuthURLParam("code_verifier", verifier),
	)
	if err != nil {
		return "", fmt.Errorf("Token-Exchange fehlgeschlagen: %w", err)
	}
	if tok.RefreshToken == "" {
		return "", fmt.Errorf("kein Refresh-Token erhalten — stelle sicher dass 'prompt=consent' erlaubt ist")
	}
	return tok.RefreshToken, nil
}

// GetAccessToken exchanges a saved refresh token for a fresh access token.
// Safe to call on every API request — the underlying TokenSource caches and
// auto-refreshes the access token when it is near expiry.
func GetAccessToken(clientID, clientSecret, refreshToken string) (string, error) {
	conf := geminiConfig(clientID, clientSecret)
	tok := &oauth2.Token{RefreshToken: refreshToken}
	src := conf.TokenSource(context.Background(), tok)
	newTok, err := src.Token()
	if err != nil {
		return "", fmt.Errorf("Token-Refresh fehlgeschlagen (neu einloggen?): %w", err)
	}
	return newTok.AccessToken, nil
}

// OpenBrowser opens url in the system default browser.
func OpenBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd, args = "open", []string{url}
	case "linux":
		cmd, args = "xdg-open", []string{url}
	default:
		cmd, args = "cmd", []string{"/c", "start", url}
	}
	exec.Command(cmd, args...).Start()
}

func pkceVerifier() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func pkceChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func randB64(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
