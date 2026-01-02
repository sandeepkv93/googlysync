package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	neturl "net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"go.uber.org/zap"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/sandeepkv93/googlysync/internal/config"
)

type idTokenClaims struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

func defaultScopes() []string {
	return []string{
		"openid",
		"email",
		"profile",
		"https://www.googleapis.com/auth/drive",
	}
}

func runOAuthFlow(ctx context.Context, cfg *config.Config, scopes []string, logger *zap.Logger) (*oauth2.Token, idTokenClaims, error) {
	state, err := randomToken(16)
	if err != nil {
		return nil, idTokenClaims{}, err
	}
	verifier, challenge, err := pkcePair()
	if err != nil {
		return nil, idTokenClaims{}, err
	}

	listener, err := net.Listen("tcp", net.JoinHostPort(cfg.OAuthRedirectHost, "0"))
	if err != nil {
		return nil, idTokenClaims{}, err
	}
	defer listener.Close()

	redirectURL := fmt.Sprintf("http://%s/oauth/callback", listener.Addr().String())
	oauthCfg := &oauth2.Config{
		ClientID:     cfg.OAuthClientID,
		ClientSecret: cfg.OAuthClientSecret,
		Endpoint:     google.Endpoint,
		RedirectURL:  redirectURL,
		Scopes:       scopes,
	}

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	mux := http.NewServeMux()
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       15 * time.Second,
	}
	mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			errCh <- errors.New("oauth state mismatch")
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		if errStr := r.URL.Query().Get("error"); errStr != "" {
			errCh <- fmt.Errorf("oauth error: %s", errStr)
			http.Error(w, "oauth error", http.StatusBadRequest)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			errCh <- errors.New("oauth code missing")
			http.Error(w, "missing code", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte("Authentication complete. You can close this window."))
		codeCh <- code
	})

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	authURL := oauthCfg.AuthCodeURL(
		state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("code_challenge", challenge),
	)
	if err := openBrowser(authURL); err != nil {
		_ = server.Shutdown(context.Background())
		return nil, idTokenClaims{}, err
	}

	var code string
	select {
	case <-ctx.Done():
		_ = server.Shutdown(context.Background())
		return nil, idTokenClaims{}, ctx.Err()
	case err := <-errCh:
		_ = server.Shutdown(context.Background())
		return nil, idTokenClaims{}, err
	case code = <-codeCh:
	}
	_ = server.Shutdown(context.Background())

	token, err := oauthCfg.Exchange(ctx, code, oauth2.SetAuthURLParam("code_verifier", verifier))
	if err != nil {
		return nil, idTokenClaims{}, err
	}

	claims := idTokenClaims{}
	if raw, ok := token.Extra("id_token").(string); ok && raw != "" {
		decoded, err := decodeJWTClaims(raw)
		if err != nil {
			logger.Warn("id_token parse failed", zap.Error(err))
		} else {
			claims = decoded
			// NOTE: We do not validate ID token signatures here because the claims
			// are used only for display metadata (email/name). Do not use these
			// fields for authorization decisions without signature verification.
		}
	}

	return token, claims, nil
}

func openBrowser(url string) error {
	parsed, err := neturl.Parse(url)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("invalid url scheme: %s", parsed.Scheme)
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		if _, err := exec.LookPath("xdg-open"); err != nil {
			return fmt.Errorf("xdg-open not found: %w", err)
		}
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

func randomToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func pkcePair() (string, string, error) {
	verifier, err := randomToken(32)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

func decodeJWTClaims(token string) (idTokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return idTokenClaims{}, errors.New("invalid token format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return idTokenClaims{}, err
	}
	var claims idTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return idTokenClaims{}, err
	}
	return claims, nil
}
