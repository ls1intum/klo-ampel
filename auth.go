package main

import (
	"cmp"
	"context"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	sessionCookie = "klo_session"
	loginCookie   = "klo_login"
)

type userKey struct{}

type session struct {
	name    string
	idToken string // kept as id_token_hint for logout at Keycloak
	expires time.Time
}

type Auth struct {
	oauth         *oauth2.Config
	idVerifier    *oidc.IDTokenVerifier
	atVerifier    *oidc.IDTokenVerifier
	endSessionURL string
	publicURL     string
	requiredRole  string
	roleClient    string // empty: realm role, otherwise a client role of this client
	ttl           time.Duration
	secure        bool

	mu       sync.Mutex
	sessions map[string]session
	now      func() time.Time
}

func NewAuth(ctx context.Context, cfg Config) (*Auth, error) {
	provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	var meta struct {
		EndSession string `json:"end_session_endpoint"`
	}
	if err := provider.Claims(&meta); err != nil {
		return nil, err
	}
	a := newAuth(cfg)
	a.oauth = &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  a.publicURL + "/auth/callback",
		Scopes:       []string{oidc.ScopeOpenID, "profile"},
	}
	a.idVerifier = provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})
	// Keycloak access tokens carry the roles but use "account" as audience.
	a.atVerifier = provider.Verifier(&oidc.Config{SkipClientIDCheck: true})
	a.endSessionURL = meta.EndSession
	return a, nil
}

func newAuth(cfg Config) *Auth {
	public := strings.TrimRight(cfg.PublicURL, "/")
	return &Auth{
		publicURL:    public,
		requiredRole: cfg.RequiredRole,
		roleClient:   cfg.RoleClient,
		ttl:          cfg.SessionTTL,
		secure:       strings.HasPrefix(public, "https://"),
		sessions:     map[string]session{},
		now:          time.Now,
	}
}

func (a *Auth) login(w http.ResponseWriter, r *http.Request) {
	state, verifier := randomID(16), oauth2.GenerateVerifier()
	// One cookie per login attempt: several tabs logging in at once must not overwrite each other.
	http.SetCookie(w, a.cookie(loginCookie+"_"+state, url.Values{
		"verifier": {verifier},
		"next":     {safeNext(r.URL.Query().Get("next"))},
	}.Encode(), 10*time.Minute))
	http.Redirect(w, r, a.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

func (a *Auth) callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("error") != "" {
		errorPage(w, http.StatusUnauthorized, "Login failed: "+q.Get("error_description"))
		return
	}
	pendingCookie := loginCookie + "_" + q.Get("state")
	c, err := r.Cookie(pendingCookie)
	if q.Get("state") == "" || err != nil {
		errorPage(w, http.StatusBadRequest, "Login expired or invalid. Please try again.")
		return
	}
	http.SetCookie(w, a.cookie(pendingCookie, "", -1))
	pending, _ := url.ParseQuery(c.Value)

	ctx := r.Context()
	tok, err := a.oauth.Exchange(ctx, q.Get("code"), oauth2.VerifierOption(pending.Get("verifier")))
	if err != nil {
		slog.Warn("token exchange failed", "err", err)
		errorPage(w, http.StatusBadGateway, "Login failed. Please try again.")
		return
	}
	rawID, _ := tok.Extra("id_token").(string)
	idTok, err := a.idVerifier.Verify(ctx, rawID)
	if err != nil {
		slog.Warn("invalid id token", "err", err)
		errorPage(w, http.StatusBadGateway, "Login failed. Please try again.")
		return
	}
	accessTok, err := a.atVerifier.Verify(ctx, tok.AccessToken)
	if err != nil {
		slog.Warn("invalid access token", "err", err)
		errorPage(w, http.StatusBadGateway, "Login failed. Please try again.")
		return
	}
	var profile struct {
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
	}
	var roles roleClaims
	if err := idTok.Claims(&profile); err != nil {
		errorPage(w, http.StatusBadGateway, "Login failed. Please try again.")
		return
	}
	if err := accessTok.Claims(&roles); err != nil {
		errorPage(w, http.StatusBadGateway, "Login failed. Please try again.")
		return
	}
	name := cmp.Or(profile.Name, profile.PreferredUsername, idTok.Subject)
	if !roles.has(a.roleClient, a.requiredRole) {
		slog.Info("login denied, missing role", "user", profile.PreferredUsername, "role", a.requiredRole)
		// Retrying would just reuse the Keycloak session, so offer to switch accounts instead.
		messagePage(w, http.StatusForbidden, fmt.Sprintf("Hi %s, you are logged in but lack the role %q needed for Klo-Ampel.", name, a.requiredRole),
			a.keycloakLogoutURL(rawID), "Log in as someone else")
		return
	}

	id := a.createSession(session{name: name, idToken: rawID})
	http.SetCookie(w, a.cookie(sessionCookie, id, a.ttl))
	slog.Info("login", "user", profile.PreferredUsername)
	http.Redirect(w, r, safeNext(pending.Get("next")), http.StatusFound)
}

func (a *Auth) logout(w http.ResponseWriter, r *http.Request) {
	target := "/"
	if c, err := r.Cookie(sessionCookie); err == nil {
		a.mu.Lock()
		s, ok := a.sessions[c.Value]
		delete(a.sessions, c.Value)
		a.mu.Unlock()
		if ok {
			target = a.keycloakLogoutURL(s.idToken)
		}
	}
	http.SetCookie(w, a.cookie(sessionCookie, "", -1))
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// keycloakLogoutURL ends the session at Keycloak too, otherwise the next login silently reuses it.
func (a *Auth) keycloakLogoutURL(idToken string) string {
	if a.endSessionURL == "" {
		return "/"
	}
	return a.endSessionURL + "?" + url.Values{
		"id_token_hint":            {idToken},
		"post_logout_redirect_uri": {a.publicURL + "/"},
	}.Encode()
}

// require lets only requests with a valid session through and puts the user name into the context.
func (a *Auth) require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ok := a.lookup(r)
		if !ok {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/auth/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, s.name)))
	})
}

func (a *Auth) createSession(s session) string {
	id := randomID(32)
	now := a.now()
	s.expires = now.Add(a.ttl)
	a.mu.Lock()
	defer a.mu.Unlock()
	for k, old := range a.sessions {
		if now.After(old.expires) {
			delete(a.sessions, k)
		}
	}
	a.sessions[id] = s
	return id
}

func (a *Auth) lookup(r *http.Request) (session, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return session{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sessions[c.Value]
	if !ok || a.now().After(s.expires) {
		delete(a.sessions, c.Value)
		return session{}, false
	}
	return s, true
}

func (a *Auth) cookie(name, value string, maxAge time.Duration) *http.Cookie {
	c := &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode}
	if maxAge < 0 {
		c.MaxAge = -1
	} else {
		c.MaxAge = int(maxAge.Seconds())
	}
	return c
}

type roleClaims struct {
	RealmAccess struct {
		Roles []string `json:"roles"`
	} `json:"realm_access"`
	ResourceAccess map[string]struct {
		Roles []string `json:"roles"`
	} `json:"resource_access"`
}

func (c roleClaims) has(client, role string) bool {
	roles := c.RealmAccess.Roles
	if client != "" {
		roles = c.ResourceAccess[client].Roles
	}
	return slices.Contains(roles, role)
}

// safeNext only allows local paths so the login cannot be abused as an open redirect.
func safeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
		return "/"
	}
	return next
}

func errorPage(w http.ResponseWriter, status int, msg string) {
	messagePage(w, status, msg, "/auth/login", "Try again")
}

func messagePage(w http.ResponseWriter, status int, msg, href, label string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!doctype html><meta name="viewport" content="width=device-width"><title>Klo-Ampel</title>
<body style="font-family:system-ui;display:grid;place-items:center;height:100vh;margin:0;background:#111;color:#eee">
<div style="text-align:center;max-width:32rem"><p style="font-size:1.25rem">%s</p><p><a style="color:#8cf" href="%s">%s</a></p></div>`, html.EscapeString(msg), html.EscapeString(href), html.EscapeString(label))
}
