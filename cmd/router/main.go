package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/SpielerNogard/claude-router/internal/api"
	"github.com/SpielerNogard/claude-router/internal/config"
	"github.com/SpielerNogard/claude-router/internal/convert"
	"github.com/SpielerNogard/claude-router/internal/discover"
	"github.com/SpielerNogard/claude-router/internal/proxy"
	"github.com/SpielerNogard/claude-router/internal/store"
	"github.com/SpielerNogard/claude-router/internal/tui"
	"github.com/SpielerNogard/claude-router/web"
	"github.com/mattn/go-isatty"
)

type deps struct {
	reg    *config.Registry
	proxy  *proxy.Proxy
	store  *store.Store
	log    *store.MsgLog
	models *discover.Registry
}

func registerRoutes(mux *http.ServeMux, d deps) {
	mux.HandleFunc("POST /v1/messages", d.proxy.Messages)
	mux.HandleFunc("POST /v1/messages/count_tokens", d.proxy.CountTokens)
	mux.HandleFunc("GET /v1/models", api.Models(d.models))
	mux.HandleFunc("POST /api/models/refresh", api.RefreshModels(d.models))
	mux.HandleFunc("GET /api/usage", api.Usage(d.store))
	mux.HandleFunc("GET /api/requests", api.Requests(d.store))
	mux.HandleFunc("GET /api/stats", api.Stats(d.store))
	mux.HandleFunc("GET /api/limits", api.Limits(func() any { return d.proxy.ProviderLimits() }))
	mux.HandleFunc("GET /api/messages", api.Messages(d.log))
	mux.HandleFunc("GET /api/messages/{id}", api.MessageDetail(d.log))
	mux.HandleFunc("GET /api/messages/by-request/{request_id}", api.MessagesByRequest(d.log))
	mux.HandleFunc("GET /api/config", api.GetConfig(d.reg))
	mux.HandleFunc("PUT /api/config", api.PutConfig(d.reg))
	mux.HandleFunc("GET /healthz", api.Health(d.proxy.BlockedProviders))
	mux.Handle("GET /ui/", web.Handler(web.Dist()))
}

// isLoopbackAddr reports whether addr is reachable only locally; a host-less ":4141" binds all interfaces.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// hasKeyProvider reports whether the router itself supplies a paid credential (auth=key).
func hasKeyProvider(cfg *config.Config) bool {
	for _, p := range cfg.Providers {
		if p.Auth == "key" {
			return true
		}
	}
	return false
}

// v1Gated reports whether /v1/ needs X-Router-Token: non-loopback listen plus an auth=key provider.
func v1Gated(listen string, cfg *config.Config) bool {
	return !isLoopbackAddr(listen) && hasKeyProvider(cfg)
}

// apiGate refuses a non-loopback start without ROUTER_API_TOKEN: /api/ (and /v1/ with auth=key) would be exposed.
func apiGate(token, listen string, hasKeyProvider bool) error {
	if token != "" || isLoopbackAddr(listen) {
		return nil
	}
	msg := fmt.Sprintf(
		"the router is set to listen on %s, which is not local only, but ROUTER_API_TOKEN is not set.\n"+
			"The /api/ endpoints can change the configuration and serve full request contents.", listen)
	if hasKeyProvider {
		msg += "\nAlso, at least one provider is configured with auth=key: without a token /v1/ would be " +
			"an open relay to the operator's paid key."
	}
	msg += "\nSet ROUTER_API_TOKEN to a secret or let the router listen on 127.0.0.1."
	return fmt.Errorf("%s", msg)
}

// withAPIToken guards /api/ with a bearer token and /v1/ (when gateV1) with X-Router-Token, since clients already use Authorization there.
func withAPIToken(token string, gateV1 func() bool, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/"):
			scheme, cred, _ := strings.Cut(r.Header.Get("Authorization"), " ")
			valid := strings.EqualFold(scheme, "Bearer") && subtle.ConstantTimeCompare([]byte(cred), []byte(token)) == 1
			if !valid {
				w.Header().Set("content-type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{
					"error": "ROUTER_API_TOKEN required: Authorization: Bearer <token>",
				})
				return
			}
		case strings.HasPrefix(r.URL.Path, "/v1/") && gateV1():
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Router-Token")), []byte(token)) != 1 {
				w.Header().Set("content-type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write(convert.ErrorBody("authentication_error",
					"X-Router-Token required: send ROUTER_API_TOKEN via ANTHROPIC_CUSTOM_HEADERS as "+
						"X-Router-Token: <token>"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// Flags may precede or follow the subcommand: "--config x check" equals "check --config x".
func runServer(configPathArg, dataDirArg string, debugOn, noTUI bool) {
	configPath, dataDir, debug := &configPathArg, &dataDirArg, &debugOn

	if _, err := os.Stat(*configPath); os.IsNotExist(err) {
		log.Fatal(configMissingMessage(*configPath))
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("startup aborted: %v", err)
	}

	token := strings.TrimSpace(os.Getenv("ROUTER_API_TOKEN"))
	if err := apiGate(token, cfg.Listen, hasKeyProvider(cfg)); err != nil {
		log.Fatal(err)
	}

	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		log.Fatalf("data directory: %v", err)
	}
	// MkdirAll does not tighten permissions of an existing directory.
	if err := os.Chmod(*dataDir, 0o700); err != nil {
		log.Printf("data directory permissions: %v", err)
	}
	// Bind before the TUI takes the terminal, so "address already in use" prints as a plain error.
	ln, err := bindAndWritePid(cfg.Listen, *dataDir)
	if err != nil {
		log.Fatal(err)
	}
	defer removePidFile(*dataDir)
	metrics, err := store.Open(filepath.Join(*dataDir, "metrics.db"))
	if err != nil {
		log.Fatalf("metrics database: %v", err)
	}
	defer metrics.Close()

	reg := config.NewRegistry(*configPath, cfg)

	msglog, err := store.OpenLog(filepath.Join(*dataDir, "messages.db"), func() store.LogLimits {
		l := reg.Get().Log
		return store.LogLimits{Level: l.Level, KeepDays: l.KeepDays, MaxSizeMB: l.MaxSizeMB, MaxBodyKB: l.MaxBodyKB}
	})
	if err != nil {
		log.Fatalf("message log: %v", err)
	}
	defer msglog.Close()
	if err := msglog.Cleanup(); err != nil {
		log.Printf("message log cleanup: %v", err)
	}
	go cleanupLoop(msglog)

	rec := proxy.NewAsyncRecorder(metrics, 1024)
	defer rec.Close()

	models := discover.NewRegistry(reg.Get, metrics)
	go refreshModels(models)

	px := proxy.New(reg.Get, rec)
	px.OnRequest(models.NoteCredential)
	px.SetMessageLog(msglog)
	px.SetDebug(*debug)

	mux := http.NewServeMux()
	registerRoutes(mux, deps{reg: reg, proxy: px, store: metrics, log: msglog, models: models})

	// Frozen at startup so a later PUT /api/config cannot pass off the bound address as loopback.
	listenAddr := reg.Get().Listen
	gateV1 := func() bool { return v1Gated(listenAddr, reg.Get()) }
	srv := &http.Server{
		Addr:    listenAddr,
		Handler: withDebug(*debug, withAPIToken(token, gateV1, mux)),
		// No ReadTimeout/WriteTimeout: long streams are the norm here.
		ReadHeaderTimeout: 20 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	serveErr := make(chan error, 1)
	go func() {
		log.Printf("claude-router listening on %s", srv.Addr)
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			serveErr <- err
			stop()
		}
	}()

	if useTUI(noTUI, *debug, isatty.IsTerminal(os.Stdin.Fd()) && foreground(os.Stdin.Fd()), isatty.IsTerminal(os.Stdout.Fd())) {
		runForeground(ctx, func(ctx context.Context) error {
			buf := tui.NewLogBuffer(200)
			log.SetOutput(buf)
			defer log.SetOutput(os.Stderr)
			return tui.Run(ctx, tuiSource{metrics, px}, buf, listenAddr)
		})
	} else {
		<-ctx.Done()
	}
	// Restore default handling so a second Ctrl+C exits even while shutdown waits on a hanging request.
	stop()
	select {
	case err := <-serveErr:
		log.Fatal(err)
	default:
	}
	log.Printf("shutting down — press Ctrl+C again to abort immediately")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	px.WaitForLogs()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v — in-flight requests were cut off", err)
	}
}

// Models change at runtime (LM Studio & co.); failures are harmless, the next run catches up.
func refreshModels(m *discover.Registry) {
	for {
		m.RefreshKeyed()
		time.Sleep(5 * time.Minute)
	}
}

func cleanupLoop(l *store.MsgLog) {
	lastFull := time.Now()
	for range time.Tick(time.Hour) {
		over, err := l.OverLimit()
		if err != nil {
			log.Printf("message log size check: %v", err)
		}
		if !cleanupDue(over, lastFull, time.Now()) {
			continue
		}
		if err := l.Cleanup(); err != nil {
			log.Printf("message log cleanup: %v", err)
		}
		lastFull = time.Now()
	}
}

// Round(0) measures wall-clock time: the monotonic clock stops during sleep on macOS.
func cleanupDue(over bool, lastFull, now time.Time) bool {
	return over || now.Round(0).Sub(lastFull.Round(0)) >= 24*time.Hour
}

// configDir uses ~/.config outside Windows (as spec and README do), not macOS's ~/Library/Application Support.
func configDir() (string, error) {
	if runtime.GOOS == "windows" {
		return os.UserConfigDir()
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config"), nil
}

func defaultConfigPath() string {
	dir, err := configDir()
	if err != nil {
		return "config.toml"
	}
	return filepath.Join(dir, "claude-router", "config.toml")
}

func defaultDataDir() string {
	dir, err := configDir()
	if err != nil {
		return "."
	}
	return filepath.Join(dir, "claude-router")
}

// configMissingMessage names the expected path and a minimal env-free example; the raw os.Open error helps nobody.
func configMissingMessage(path string) string {
	return fmt.Sprintf(
		"no config file found at %s.\n"+
			"Create it, e.g. with this minimal content that starts without environment variables:\n\n"+
			"[providers.subscription]\n"+
			"format = \"anthropic\"\n"+
			"auth   = \"passthrough\"\n", path)
}
