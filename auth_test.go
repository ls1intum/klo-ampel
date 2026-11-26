package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// fakeKeycloak is a minimal OIDC provider that issues Keycloak-shaped tokens with the given realm roles.
func fakeKeycloak(t *testing.T, roles []string) *httptest.Server {
	t.Helper()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: "k1"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	sign := func(claims map[string]any) string {
		b, _ := json.Marshal(claims)
		obj, _ := signer.Sign(b)
		s, _ := obj.CompactSerialize()
		return s
	}

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 srv.URL,
			"authorization_endpoint": srv.URL + "/auth",
			"token_endpoint":         srv.URL + "/token",
			"jwks_uri":               srv.URL + "/jwks",
			"end_session_endpoint":   srv.URL + "/logout",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("code") != "good-code" || r.Form.Get("code_verifier") == "" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		now := time.Now().Unix()
		base := map[string]any{"iss": srv.URL, "sub": "user-1", "iat": now, "exp": now + 300}
		id := map[string]any{"aud": "klo", "name": "Ada Lovelace", "preferred_username": "ada"}
		access := map[string]any{"aud": "account", "azp": "klo", "realm_access": map[string]any{"roles": roles}}
		for k, v := range base {
			id[k], access[k] = v, v
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": sign(access), "id_token": sign(id), "token_type": "Bearer", "expires_in": 300,
		})
	})
	return srv
}

func newTestApp(t *testing.T, roles []string) (http.Handler, *Auth) {
	t.Helper()
	idp := fakeKeycloak(t, roles)
	auth, err := NewAuth(context.Background(), Config{
		PublicURL: "http://app.test", IssuerURL: idp.URL, ClientID: "klo", ClientSecret: "s",
		RequiredRole: "klo-ampel", SessionTTL: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	return newHandler(NewStore(), auth, t.TempDir()), auth
}

func get(h http.Handler, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func cookieNamed(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// startLogin runs /auth/login and returns the state sent to Keycloak and the login cookie.
func startLogin(t *testing.T, h http.Handler, next string) (string, *http.Cookie) {
	t.Helper()
	rec := get(h, "/auth/login?next="+url.QueryEscape(next))
	if rec.Code != http.StatusFound {
		t.Fatalf("login: %d", rec.Code)
	}
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if loc.Query().Get("code_challenge_method") != "S256" || loc.Query().Get("redirect_uri") != "http://app.test/auth/callback" {
		t.Fatalf("unexpected auth URL %s", loc)
	}
	state := loc.Query().Get("state")
	return state, cookieNamed(rec, loginCookie+"_"+state)
}

func TestLoginFlow(t *testing.T) {
	h, _ := newTestApp(t, []string{"offline_access", "klo-ampel"})
	state, loginC := startLogin(t, h, "/rooms/abc")

	rec := get(h, "/auth/callback?code=good-code&state="+state, loginC)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/rooms/abc" {
		t.Fatalf("callback: %d %q %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	sess := cookieNamed(rec, sessionCookie)
	if sess == nil || !sess.HttpOnly || sess.MaxAge != 3600 {
		t.Fatalf("session cookie = %+v", sess)
	}

	if rec := get(h, "/api/me", sess); !strings.Contains(rec.Body.String(), "Ada Lovelace") {
		t.Fatalf("me: %d %s", rec.Code, rec.Body)
	}

	req := httptest.NewRequest("POST", "/auth/logout", nil)
	req.AddCookie(sess)
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if loc := out.Header().Get("Location"); !strings.Contains(loc, "/logout?") || !strings.Contains(loc, "id_token_hint=") {
		t.Fatalf("logout redirect = %q", loc)
	}
	if rec := get(h, "/api/me", sess); rec.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout: %d", rec.Code)
	}
}

// After a restart, every open tab starts a login at the same time.
func TestConcurrentLogins(t *testing.T) {
	h, _ := newTestApp(t, []string{"klo-ampel"})
	state1, c1 := startLogin(t, h, "/rooms/a")
	state2, c2 := startLogin(t, h, "/rooms/b")
	for _, tc := range []struct {
		state string
		c     *http.Cookie
		want  string
	}{{state2, c2, "/rooms/b"}, {state1, c1, "/rooms/a"}} {
		rec := get(h, "/auth/callback?code=good-code&state="+tc.state, c1, c2)
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != tc.want {
			t.Fatalf("callback for %s: %d %q", tc.want, rec.Code, rec.Header().Get("Location"))
		}
	}
}

func TestLoginDeniedWithoutRole(t *testing.T) {
	h, _ := newTestApp(t, []string{"offline_access"})
	state, loginC := startLogin(t, h, "/")
	rec := get(h, "/auth/callback?code=good-code&state="+state, loginC)
	if rec.Code != http.StatusForbidden || cookieNamed(rec, sessionCookie) != nil {
		t.Fatalf("callback without role: %d, cookies %v", rec.Code, rec.Result().Cookies())
	}
	if !strings.Contains(rec.Body.String(), "/logout?id_token_hint=") {
		t.Fatalf("403 page should offer a Keycloak logout to switch accounts: %s", rec.Body)
	}
}

func TestCallbackRejectsBadState(t *testing.T) {
	h, _ := newTestApp(t, []string{"klo-ampel"})
	_, loginC := startLogin(t, h, "/")
	if rec := get(h, "/auth/callback?code=good-code&state=forged", loginC); rec.Code != http.StatusBadRequest {
		t.Fatalf("forged state: %d", rec.Code)
	}
	if rec := get(h, "/auth/callback?code=good-code&state=x"); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing login cookie: %d", rec.Code)
	}
}

func TestRequire(t *testing.T) {
	h, auth := newTestApp(t, nil)

	if rec := get(h, "/api/me"); rec.Code != http.StatusUnauthorized {
		t.Errorf("api without session: %d", rec.Code)
	}
	rec := get(h, "/rooms/abc?x=1")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/auth/login?next="+url.QueryEscape("/rooms/abc?x=1") {
		t.Errorf("page without session: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if rec := get(h, "/healthz"); rec.Code != http.StatusOK {
		t.Errorf("healthz: %d", rec.Code)
	}

	id := auth.createSession(session{name: "Ada"})
	c := &http.Cookie{Name: sessionCookie, Value: id}
	if rec := get(h, "/api/me", c); rec.Code != http.StatusOK {
		t.Errorf("valid session: %d", rec.Code)
	}
	auth.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if rec := get(h, "/api/me", c); rec.Code != http.StatusUnauthorized {
		t.Errorf("expired session: %d", rec.Code)
	}
}

func TestCrossOriginWritesBlocked(t *testing.T) {
	h, auth := newTestApp(t, nil)
	id := auth.createSession(session{name: "Ada"})
	req := httptest.NewRequest("POST", "/api/rooms", strings.NewReader(`{"name":"x"}`))
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: id})
	req.Header.Set("Sec-Fetch-Site", "same-site") // e.g. another app on a sibling subdomain
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin POST: %d", rec.Code)
	}
}

func TestRoleClaims(t *testing.T) {
	var c roleClaims
	json.Unmarshal([]byte(`{"realm_access":{"roles":["staff"]},"resource_access":{"klo":{"roles":["user"]}}}`), &c)
	cases := []struct {
		client, role string
		want         bool
	}{
		{"", "staff", true},
		{"", "user", false},
		{"klo", "user", true},
		{"klo", "staff", false},
		{"other", "user", false},
	}
	for _, tc := range cases {
		if got := c.has(tc.client, tc.role); got != tc.want {
			t.Errorf("has(%q, %q) = %v", tc.client, tc.role, got)
		}
	}
}

func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{
		"/rooms/a":          "/rooms/a",
		"":                  "/",
		"https://evil.test": "/",
		"//evil.test":       "/",
		"/\\evil.test":      "/",
	} {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoadConfig(t *testing.T) {
	env := map[string]string{
		"PUBLIC_URL": "https://klo.example", "OIDC_ISSUER_URL": "https://kc/realms/x",
		"OIDC_CLIENT_ID": "klo", "OIDC_REQUIRED_ROLE": "staff",
	}
	cfg, err := loadConfig(func(k string) string { return env[k] })
	if err != nil || cfg.SessionTTL != 12*time.Hour || cfg.ListenAddr != ":8080" {
		t.Fatalf("cfg = %+v, %v", cfg, err)
	}
	delete(env, "OIDC_REQUIRED_ROLE")
	if _, err := loadConfig(func(k string) string { return env[k] }); err == nil {
		t.Fatal("missing role must fail")
	}
}
