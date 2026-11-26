package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type Config struct {
	ListenAddr   string
	PublicURL    string
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RequiredRole string
	RoleClient   string
	SessionTTL   time.Duration
	StaticDir    string
}

func loadConfig(getenv func(string) string) (Config, error) {
	cfg := Config{
		ListenAddr:   cmp.Or(getenv("LISTEN_ADDR"), ":8080"),
		PublicURL:    getenv("PUBLIC_URL"),
		IssuerURL:    getenv("OIDC_ISSUER_URL"),
		ClientID:     getenv("OIDC_CLIENT_ID"),
		ClientSecret: getenv("OIDC_CLIENT_SECRET"),
		RequiredRole: getenv("OIDC_REQUIRED_ROLE"),
		RoleClient:   getenv("OIDC_ROLE_CLIENT"),
		StaticDir:    cmp.Or(getenv("STATIC_DIR"), "web/dist"),
	}
	ttl, err := time.ParseDuration(cmp.Or(getenv("SESSION_TTL"), "12h"))
	if err != nil {
		return cfg, fmt.Errorf("SESSION_TTL: %w", err)
	}
	cfg.SessionTTL = ttl
	for name, v := range map[string]string{
		"PUBLIC_URL":         cfg.PublicURL,
		"OIDC_ISSUER_URL":    cfg.IssuerURL,
		"OIDC_CLIENT_ID":     cfg.ClientID,
		"OIDC_REQUIRED_ROLE": cfg.RequiredRole,
	} {
		if v == "" {
			return cfg, fmt.Errorf("%s is required", name)
		}
	}
	return cfg, nil
}

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	auth, err := NewAuth(ctx, cfg)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           newHandler(NewStore(), auth, cfg.StaticDir),
		ReadHeaderTimeout: 10 * time.Second,
		// Open SSE streams end when ctx is cancelled, otherwise Shutdown would wait for them.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()
	slog.Info("listening", "addr", cfg.ListenAddr, "public_url", cfg.PublicURL)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
